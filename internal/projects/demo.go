package projects

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// SeedDemo fills an empty account with about a year and a half of realistic
// projects, so that the dashboard and the lists can be tried out. It refuses
// to touch an account that already has projects.
func SeedDemo(ctx context.Context, s *Store, uid int64, today time.Time) (int, error) {
	existing, err := s.ListProjects(ctx, uid, "")
	if err != nil {
		return 0, err
	}
	if len(existing) > 0 {
		return 0, errors.New("the account already has projects; demo data is only loaded into empty accounts")
	}

	rng := rand.New(rand.NewPCG(20260924, 42))
	day := func(t time.Time) string { return t.Format("2006-01-02") }

	towns := []string{"Rafelguaraf", "Xàtiva", "Alzira", "Carcaixent", "l'Alcúdia", "Manuel", "la Pobla Llarga", "Sumacàrcer"}
	names := []string{
		"Reforma de bany", "Cuina nova", "Tancat de pati", "Façana i balcons", "Solera del magatzem",
		"Reforma integral del pis", "Escala de la comunitat", "Paret de bloc", "Terrassa i impermeabilització",
		"Envà i regates", "Porxo de la caseta", "Canvi de rajoles", "Barbacoa d'obra", "Muret de tanca",
	}
	clients := []string{"Maria Ferrer", "Vicent Soler", "Pepa Garcia", "Toni Martí", "Amparo Sanchis", "Rafa Bosch", "Carme Llopis", ""}
	type expense struct {
		category string
		what     []string
		min, max int // euros
	}
	expenses := []expense{
		{"Material", []string{"Ciment i arena", "Blocs i maons", "Morter cola", "Perfils i rastrells"}, 60, 900},
		{"Fontaneria", []string{"Plat de dutxa", "Canonades", "Aixeta i sifó", "Desguàs"}, 80, 700},
		{"Electricitat", []string{"Punts de llum", "Endolls", "Quadre"}, 60, 500},
		{"Enrajolat", []string{"Rajola de paret", "Paviment", "Sòcol"}, 150, 1200},
		{"Pintura", []string{"Pintura plàstica", "Esmalt"}, 40, 300},
		{"Contenidor de runa", []string{"Contenidor 5 m³"}, 120, 220},
		{"Lloguer de maquinària", []string{"Formigonera", "Martell pneumàtic"}, 30, 160},
	}
	markups := []int{0, 10, 15, 20, 25, 30}
	notes := []string{"Bizum", "Efectiu", "Transferència", "Senyal", ""}

	created := 0
	start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location()).AddDate(0, -17, 0)
	for m := 0; m < 18; m++ {
		month := start.AddDate(0, m, 0)
		perMonth := 1 + rng.IntN(2)
		for k := 0; k < perMonth; k++ {
			begin := month.AddDate(0, 0, rng.IntN(20))
			if begin.After(today) {
				continue
			}
			in := ProjectInput{
				Name:       names[rng.IntN(len(names))],
				TownName:   towns[rng.IntN(len(towns))],
				ClientName: clients[rng.IntN(len(clients))],
				StartedOn:  day(begin),
			}
			if err := in.normalize(day(today)); err != nil {
				return created, err
			}
			p, err := s.CreateProject(ctx, uid, in)
			if err != nil {
				return created, err
			}
			created++

			var price int64
			nItems := 3 + rng.IntN(5)
			for i := 0; i < nItems; i++ {
				when := begin.AddDate(0, 0, rng.IntN(35))
				if when.After(today) {
					when = today
				}
				var item ItemInput
				if rng.IntN(3) == 0 {
					hours := float64(8 * (1 + rng.IntN(5)))
					rate := int64(22 + rng.IntN(9))
					item = ItemInput{Kind: KindLabor, Description: "Mà d'obra", Hours: &hours,
						PriceCents: int64(hours) * rate * 100, ItemDate: day(when)}
				} else {
					e := expenses[rng.IntN(len(expenses))]
					cost := int64(e.min+rng.IntN(e.max-e.min+1)) * 100
					markup := int64(markups[rng.IntN(len(markups))])
					item = ItemInput{Kind: KindExpense, CategoryName: e.category, Description: e.what[rng.IntN(len(e.what))],
						CostCents: cost, PriceCents: cost + cost*markup/100, ItemDate: day(when)}
				}
				if err := item.normalize(day(today)); err != nil {
					return created, err
				}
				if _, _, err := s.CreateItem(ctx, uid, p.ID, item); err != nil {
					return created, err
				}
				price += item.PriceCents
			}

			// Older projects are finished and mostly paid; recent ones are open.
			age := today.Sub(begin)
			finished := age > 75*24*time.Hour
			paidShare := 0.3 + rng.Float64()*0.5
			if finished {
				paidShare = 1
				if rng.IntN(5) == 0 {
					paidShare = 0.7
				}
			}
			toPay := int64(float64(price) * paidShare)
			installments := 1 + rng.IntN(3)
			for i := 0; i < installments && toPay > 0; i++ {
				amount := toPay / int64(installments-i)
				when := begin.AddDate(0, 0, 10+rng.IntN(50)+i*15)
				if when.After(today) {
					when = today
				}
				pay := PaymentInput{AmountCents: amount, PaidOn: day(when), Note: notes[rng.IntN(len(notes))]}
				if err := pay.normalize(day(today)); err != nil {
					return created, err
				}
				if _, _, err := s.CreatePayment(ctx, uid, p.ID, pay); err != nil {
					return created, err
				}
				toPay -= amount
			}
			if finished {
				if _, err := s.SetStatus(ctx, uid, p.ID, StatusFinished); err != nil {
					return created, err
				}
			}
		}
	}

	// The example from the specification, as an open project of this month.
	p, err := s.CreateProject(ctx, uid, ProjectInput{Name: "Reforma del bany", TownName: "Rafelguaraf",
		ClientName: "Família Vidal", StartedOn: day(today)})
	if err != nil {
		return created, err
	}
	created++
	hours := 20.0
	for _, it := range []ItemInput{
		{Kind: KindExpense, CategoryName: "Material", Description: "Muratura", CostCents: 10000, PriceCents: 13000, ItemDate: day(today)},
		{Kind: KindLabor, Description: "Muratura", Hours: &hours, PriceCents: 30000, ItemDate: day(today)},
		{Kind: KindExpense, CategoryName: "Fontaneria", Description: "Instal·lació del bany", CostCents: 15000, PriceCents: 17000, ItemDate: day(today)},
	} {
		if _, _, err := s.CreateItem(ctx, uid, p.ID, it); err != nil {
			return created, fmt.Errorf("example project: %w", err)
		}
	}
	return created, nil
}
