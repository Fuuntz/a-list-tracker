package api

import "testing"

func TestMembershipPeriodsCannotOverlap(t *testing.T) {
	end := "2026-06-30"
	_, err := validatePeriods([]MembershipPeriod{
		{StartsOn: "2026-01-01", EndsOn: &end, MonthlyCost: 25},
		{StartsOn: "2026-06-15", MonthlyCost: 30},
	})
	if err == nil {
		t.Fatal("expected overlapping membership periods to be rejected")
	}
}

func TestMembershipPeriodsAreSorted(t *testing.T) {
	firstEnd := "2026-05-31"
	periods, err := validatePeriods([]MembershipPeriod{
		{StartsOn: "2026-06-01", MonthlyCost: 30},
		{StartsOn: "2026-01-01", EndsOn: &firstEnd, MonthlyCost: 25},
	})
	if err != nil {
		t.Fatalf("validatePeriods returned an error: %v", err)
	}
	if got := periods[0].startsOn.Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("expected periods to be sorted, first start was %s", got)
	}
}
