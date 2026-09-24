package projects

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/lucabodd/obrabi/internal/platform/money"
)

// PDFVariant selects what the printed summary shows.
type PDFVariant string

const (
	// PDFInternal is Nando's own report: costs, prices and margins.
	PDFInternal PDFVariant = "intern"
	// PDFClient can be handed to the client: only prices, payments and what
	// is still pending — never costs or margins.
	PDFClient PDFVariant = "client"
)

type rgb struct{ r, g, b int }

var (
	colInk    = rgb{31, 41, 55}
	colMuted  = rgb{107, 114, 128}
	colAccent = rgb{180, 83, 42}
	colFill   = rgb{248, 245, 241}
	colLine   = rgb{229, 223, 216}
	colGood   = rgb{21, 128, 61}
	colBad    = rgb{185, 28, 28}
)

const (
	pageMargin = 15.0
	rowHeight  = 7.0
)

type pdfDoc struct {
	*fpdf.Fpdf
	tr func(string) string
}

func (p *pdfDoc) text(c rgb) { p.SetTextColor(c.r, c.g, c.b) }
func (p *pdfDoc) fill(c rgb) { p.SetFillColor(c.r, c.g, c.b) }
func (p *pdfDoc) draw(c rgb) { p.SetDrawColor(c.r, c.g, c.b) }
func (p *pdfDoc) width() float64 {
	w, _ := p.GetPageSize()
	return w - 2*pageMargin
}

// cell writes UTF-8 text (translated to the cp1252 core-font encoding),
// shortening it with an ellipsis when it does not fit in w.
func (p *pdfDoc) cell(w, h float64, s, border string, ln int, align string, fill bool) {
	p.CellFormat(w, h, p.fit(p.tr(s), w-2), border, ln, align, fill, 0, "")
}

func (p *pdfDoc) fit(s string, w float64) string {
	if w <= 0 || p.GetStringWidth(s) <= w {
		return s
	}
	const ellipsis = "\x85" // "…" in cp1252
	for len(s) > 0 && p.GetStringWidth(s+ellipsis) > w {
		s = s[:len(s)-1]
	}
	return strings.TrimRight(s, " ") + ellipsis
}

// ensureSpace starts a new page when fewer than h millimetres are left.
func (p *pdfDoc) ensureSpace(h float64) bool {
	_, pageH := p.GetPageSize()
	_, _, _, bottom := p.GetMargins()
	if p.GetY()+h > pageH-bottom {
		p.AddPage()
		return true
	}
	return false
}

// RenderPDF builds the printable summary of a project.
func RenderPDF(d ProjectDetail, variant PDFVariant, now time.Time) ([]byte, error) {
	f := fpdf.New("P", "mm", "A4", "")
	p := &pdfDoc{Fpdf: f, tr: f.UnicodeTranslatorFromDescriptor("")}

	title := d.Name
	if variant == PDFClient {
		p.SetSubject("Resum per al client", true)
	} else {
		p.SetSubject("Informe intern", true)
	}
	p.SetTitle(title, true)
	p.SetCreator("Obrabi", true)
	p.SetCreationDate(now)
	p.SetMargins(pageMargin, pageMargin, pageMargin)
	p.SetAutoPageBreak(true, 18)
	p.AliasNbPages("{nb}")
	p.SetFooterFunc(func() {
		p.SetY(-12)
		p.SetFont("Helvetica", "", 8)
		p.text(colMuted)
		p.cell(p.width()/2, 5, d.Name+" · "+d.Town.Name, "", 0, "L", false)
		p.cell(p.width()/2, 5, fmt.Sprintf("Pàgina %d de {nb}", p.PageNo()), "", 0, "R", false)
	})
	p.AddPage()

	writeHeader(p, d, variant, now)
	writeSummary(p, d, variant)
	writeItems(p, d, variant)
	writePayments(p, d, variant)
	if variant == PDFInternal && d.Notes != "" {
		writeNotes(p, d.Notes)
	}

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeHeader(p *pdfDoc, d ProjectDetail, variant PDFVariant, now time.Time) {
	w := p.width()
	// Accent rule across the top.
	p.fill(colAccent)
	p.Rect(pageMargin, 10, w, 1.2, "F")

	p.SetY(15)
	p.SetFont("Helvetica", "B", 10)
	p.text(colAccent)
	p.cell(w/2, 5, "OBRABI", "", 0, "L", false)
	p.SetFont("Helvetica", "", 8)
	p.text(colMuted)
	kind := "Informe intern"
	if variant == PDFClient {
		kind = "Resum per al client"
	}
	p.cell(w/2, 5, kind+" · "+formatDate(now.Format("2006-01-02")), "", 1, "R", false)

	p.Ln(4)
	p.SetFont("Helvetica", "B", 20)
	p.text(colInk)
	p.MultiCell(w, 9, p.tr(d.Name), "", "L", false)

	p.SetFont("Helvetica", "", 10)
	p.text(colMuted)
	parts := []string{d.Town.Name}
	if d.ClientName != "" {
		parts = append(parts, "Client: "+d.ClientName)
	}
	if d.Address != "" {
		parts = append(parts, d.Address)
	}
	if d.ClientPhone != "" {
		parts = append(parts, "Tel. "+d.ClientPhone)
	}
	p.MultiCell(w, 5.5, p.tr(strings.Join(parts, " · ")), "", "L", false)

	status := "Obra en curs"
	if d.Status == StatusFinished && d.FinishedAt != nil {
		status = "Obra finalitzada el " + formatDate(d.FinishedAt.In(now.Location()).Format("2006-01-02"))
	}
	p.MultiCell(w, 5.5, p.tr(status+" · Inici: "+formatDate(d.StartedOn)), "", "L", false)
	p.Ln(5)
}

type figure struct {
	label string
	cents int64
	color rgb
}

func writeSummary(p *pdfDoc, d ProjectDetail, variant PDFVariant) {
	t := d.Totals
	pendingLabel := "Pendent de cobrar"
	if variant == PDFClient {
		pendingLabel = "Pendent de pagament"
	}
	pending, pendingColor := t.PendingCents, colAccent
	if pending < 0 {
		pendingLabel, pending, pendingColor = "A favor del client", -pending, colInk
	} else if pending == 0 {
		pendingColor = colGood
	}

	var figs []figure
	if variant == PDFClient {
		figs = []figure{
			{"Total de l'obra", t.PriceCents, colInk},
			{"Pagat", t.CollectedCents, colInk},
			{pendingLabel, pending, pendingColor},
		}
	} else {
		benefitColor := colGood
		if t.BenefitCents < 0 {
			benefitColor = colBad
		}
		figs = []figure{
			{"Total client", t.PriceCents, colInk},
			{"Despeses", t.CostCents, colInk},
			{"Benefici", t.BenefitCents, benefitColor},
			{"Cobrat", t.CollectedCents, colInk},
			{pendingLabel, pending, pendingColor},
		}
	}

	const gap, boxH = 3.0, 17.0
	w := p.width()
	boxW := (w - gap*float64(len(figs)-1)) / float64(len(figs))
	y := p.GetY()
	for i, fg := range figs {
		x := pageMargin + float64(i)*(boxW+gap)
		p.fill(colFill)
		p.draw(colLine)
		p.SetLineWidth(0.2)
		p.RoundedRect(x, y, boxW, boxH, 2, "1234", "FD")
		p.SetXY(x+3, y+2.5)
		p.SetFont("Helvetica", "", 8)
		p.text(colMuted)
		p.cell(boxW-6, 4, fg.label, "", 2, "L", false)
		p.SetX(x + 3)
		p.SetFont("Helvetica", "B", 12)
		p.text(fg.color)
		p.cell(boxW-6, 8, money.Format(fg.cents), "", 0, "L", false)
	}
	p.SetXY(pageMargin, y+boxH+4)

	if variant == PDFInternal {
		p.SetFont("Helvetica", "", 9)
		p.text(colMuted)
		line := fmt.Sprintf("Balanç de caixa (cobrat menys despeses): %s", money.Format(t.BalanceCents))
		if t.LaborHours > 0 {
			line += fmt.Sprintf(" · Mà d'obra: %s h, %s", formatHours(t.LaborHours), money.Format(t.LaborCents))
		}
		p.cell(w, 5, line, "", 1, "L", false)
	}
	p.Ln(4)
}

type column struct {
	title string
	width float64
	align string
}

func tableHeader(p *pdfDoc, cols []column) {
	p.SetFont("Helvetica", "B", 8.5)
	p.text(colMuted)
	p.draw(colLine)
	p.SetLineWidth(0.3)
	for i, c := range cols {
		ln := 0
		if i == len(cols)-1 {
			ln = 1
		}
		p.cell(c.width, rowHeight, strings.ToUpper(c.title), "B", ln, c.align, false)
	}
}

func sectionTitle(p *pdfDoc, title string) {
	p.ensureSpace(rowHeight*3 + 10)
	p.SetFont("Helvetica", "B", 12)
	p.text(colInk)
	p.cell(p.width(), 8, title, "", 1, "L", false)
}

func writeItems(p *pdfDoc, d ProjectDetail, variant PDFVariant) {
	sectionTitle(p, "Partides")
	w := p.width()
	var cols []column
	if variant == PDFClient {
		cols = []column{{"Data", 22, "L"}, {"Concepte", w - 22 - 40, "L"}, {"Import", 40, "R"}}
	} else {
		cols = []column{{"Data", 22, "L"}, {"Concepte", w - 22 - 3*26, "L"}, {"Cost", 26, "R"}, {"Preu", 26, "R"}, {"Benefici", 26, "R"}}
	}
	tableHeader(p, cols)

	if len(d.Items) == 0 {
		p.SetFont("Helvetica", "I", 9)
		p.text(colMuted)
		p.cell(w, rowHeight, "Encara no hi ha partides.", "", 1, "L", false)
		p.Ln(4)
		return
	}

	// Oldest first reads more naturally on paper.
	for i := len(d.Items) - 1; i >= 0; i-- {
		it := d.Items[i]
		if p.ensureSpace(rowHeight) {
			tableHeader(p, cols)
		}
		zebra := (len(d.Items)-1-i)%2 == 1
		p.fill(colFill)
		p.SetFont("Helvetica", "", 9)
		p.text(colInk)
		p.cell(cols[0].width, rowHeight, formatDate(it.ItemDate), "", 0, "L", zebra)
		if variant == PDFClient {
			p.cell(cols[1].width, rowHeight, itemConcept(it), "", 0, "L", zebra)
			p.cell(cols[2].width, rowHeight, money.Format(it.PriceCents), "", 1, "R", zebra)
			continue
		}
		p.cell(cols[1].width, rowHeight, itemConcept(it), "", 0, "L", zebra)
		p.text(colMuted)
		p.cell(cols[2].width, rowHeight, money.Format(it.CostCents), "", 0, "R", zebra)
		p.text(colInk)
		p.cell(cols[3].width, rowHeight, money.Format(it.PriceCents), "", 0, "R", zebra)
		benefit := it.PriceCents - it.CostCents
		if benefit < 0 {
			p.text(colBad)
		} else {
			p.text(colGood)
		}
		p.cell(cols[4].width, rowHeight, money.Format(benefit), "", 1, "R", zebra)
	}

	// Totals row.
	p.ensureSpace(rowHeight)
	p.draw(colLine)
	p.SetFont("Helvetica", "B", 9)
	p.text(colInk)
	t := d.Totals
	if variant == PDFClient {
		p.cell(cols[0].width+cols[1].width, rowHeight, "Total", "T", 0, "L", false)
		p.cell(cols[2].width, rowHeight, money.Format(t.PriceCents), "T", 1, "R", false)
	} else {
		p.cell(cols[0].width+cols[1].width, rowHeight, "Total", "T", 0, "L", false)
		p.cell(cols[2].width, rowHeight, money.Format(t.CostCents), "T", 0, "R", false)
		p.cell(cols[3].width, rowHeight, money.Format(t.PriceCents), "T", 0, "R", false)
		p.cell(cols[4].width, rowHeight, money.Format(t.BenefitCents), "T", 1, "R", false)
	}
	p.Ln(6)
}

func writePayments(p *pdfDoc, d ProjectDetail, variant PDFVariant) {
	// Seen from the client's side, collections are payments.
	title, totalLabel, pendingLabel := "Cobraments", "Total cobrat", "Pendent de cobrar"
	if variant == PDFClient {
		title, totalLabel, pendingLabel = "Pagaments", "Total pagat", "Pendent de pagament"
	}
	sectionTitle(p, title)
	w := p.width()
	cols := []column{{"Data", 22, "L"}, {"Nota", w - 22 - 40, "L"}, {"Import", 40, "R"}}
	tableHeader(p, cols)
	if len(d.Payments) == 0 {
		p.SetFont("Helvetica", "I", 9)
		p.text(colMuted)
		p.cell(w, rowHeight, "Encara no hi ha "+strings.ToLower(title)+".", "", 1, "L", false)
		p.Ln(4)
		return
	}
	for i := len(d.Payments) - 1; i >= 0; i-- {
		pay := d.Payments[i]
		if p.ensureSpace(rowHeight) {
			tableHeader(p, cols)
		}
		zebra := (len(d.Payments)-1-i)%2 == 1
		p.fill(colFill)
		p.SetFont("Helvetica", "", 9)
		p.text(colInk)
		note := pay.Note
		if note == "" {
			note = "Pagament"
		}
		p.cell(cols[0].width, rowHeight, formatDate(pay.PaidOn), "", 0, "L", zebra)
		p.cell(cols[1].width, rowHeight, note, "", 0, "L", zebra)
		p.cell(cols[2].width, rowHeight, money.Format(pay.AmountCents), "", 1, "R", zebra)
	}
	p.ensureSpace(rowHeight * 2)
	p.SetFont("Helvetica", "B", 9)
	p.text(colInk)
	p.draw(colLine)
	p.cell(cols[0].width+cols[1].width, rowHeight, totalLabel, "T", 0, "L", false)
	p.cell(cols[2].width, rowHeight, money.Format(d.Totals.CollectedCents), "T", 1, "R", false)
	label, pending := pendingLabel, d.Totals.PendingCents
	if pending < 0 {
		label, pending = "A favor del client", -pending
	}
	p.text(colAccent)
	p.cell(cols[0].width+cols[1].width, rowHeight, label, "", 0, "L", false)
	p.cell(cols[2].width, rowHeight, money.Format(pending), "", 1, "R", false)
	p.Ln(6)
}

func writeNotes(p *pdfDoc, notes string) {
	sectionTitle(p, "Notes")
	p.SetFont("Helvetica", "", 9.5)
	p.text(colInk)
	p.MultiCell(p.width(), 5, p.tr(notes), "", "L", false)
}

func itemConcept(it Item) string {
	var head string
	if it.Kind == KindLabor {
		head = "Mà d'obra"
		if it.Hours != nil && *it.Hours > 0 {
			head += " (" + formatHours(*it.Hours) + " h)"
		}
	} else if it.Category != nil {
		head = it.Category.Name
	}
	if it.Description != "" {
		if head == "" {
			return it.Description
		}
		return head + " — " + it.Description
	}
	return head
}

// formatDate turns 2026-09-24 into 24/09/2026.
func formatDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02/01/2006")
}

// formatHours renders 7.5 as "7,5" and 20 as "20".
func formatHours(h float64) string {
	s := strconv.FormatFloat(h, 'f', -1, 64)
	return strings.Replace(s, ".", ",", 1)
}

// PDFFilename builds an ASCII file name such as
// "obra-reforma-bany-rafelguaraf-client.pdf".
func PDFFilename(d ProjectDetail, variant PDFVariant) string {
	name := slug(d.Name + " " + d.Town.Name)
	if name == "" {
		name = strconv.FormatInt(d.ID, 10)
	}
	if variant == PDFClient {
		name += "-client"
	}
	return "obra-" + name + ".pdf"
}

var accentFold = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ä", "a",
	"è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ò", "o", "ó", "o", "ô", "o", "ö", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "·", "",
)

func slug(s string) string {
	s = accentFold.Replace(strings.ToLower(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	return out
}
