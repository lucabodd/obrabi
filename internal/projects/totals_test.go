package projects

import "testing"

// The example from the specification: a renovation in Rafelguaraf.
func TestComputeTotalsRafelguaraf(t *testing.T) {
	items := []struct{ cost, price int64 }{
		{10000, 13000}, // material: paid 100, sold 130
		{15000, 17000}, // fontaneria: paid 150, sold 170
		{0, 30000},     // mà d'obra: 300
	}
	var s sums
	for _, it := range items {
		s.Cost += it.cost
		s.Price += it.price
		s.Items++
	}
	s.Labor = 30000
	s.Hours = 20

	got := ComputeTotals(s)
	want := Totals{
		CostCents:      25000,
		PriceCents:     60000,
		BenefitCents:   35000, // 30 + 20 + 300
		CollectedCents: 0,
		PendingCents:   60000,
		BalanceCents:   -25000,
		LaborHours:     20,
		LaborCents:     30000,
		Items:          3,
	}
	if got != want {
		t.Fatalf("totals = %+v\nwant     %+v", got, want)
	}

	// The client pays 200 on account, then the rest.
	s.Collected, s.Payments = 20000, 1
	got = ComputeTotals(s)
	if got.PendingCents != 40000 || got.BalanceCents != -5000 {
		t.Fatalf("after 200 paid: pending %d balance %d", got.PendingCents, got.BalanceCents)
	}
	s.Collected, s.Payments = 60000, 2
	got = ComputeTotals(s)
	if got.PendingCents != 0 || got.BalanceCents != got.BenefitCents {
		t.Fatalf("fully paid: pending %d balance %d benefit %d", got.PendingCents, got.BalanceCents, got.BenefitCents)
	}
}
