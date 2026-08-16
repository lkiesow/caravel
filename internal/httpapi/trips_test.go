package httpapi

import "testing"

func strptr(s string) *string { return &s }

// tripRequest.validate is the single gate both handleCreateTrip and
// handleUpdateTrip run their body through, so covering it covers create and
// update alike.
func TestTripRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     tripRequest
		wantErr string
	}{
		{
			name: "title only",
			req:  tripRequest{Title: "Iceland"},
		},
		{
			name: "blank title rejected",
			req:  tripRequest{Title: "   "},
			// Whitespace-only titles were already rejected; asserted here so
			// the added date check can't quietly displace it.
			wantErr: "title is required",
		},
		{
			name: "well-formed range",
			req:  tripRequest{Title: "Iceland", StartDate: strptr("2026-08-20"), EndDate: strptr("2026-08-23")},
		},
		{
			name: "same day start and end",
			req:  tripRequest{Title: "Day trip", StartDate: strptr("2026-08-20"), EndDate: strptr("2026-08-20")},
		},
		{
			name:    "end before start",
			req:     tripRequest{Title: "Iceland", StartDate: strptr("2026-08-20"), EndDate: strptr("2026-08-01")},
			wantErr: "end date must not be before start date",
		},
		{
			name:    "end before start across years",
			req:     tripRequest{Title: "Iceland", StartDate: strptr("2027-01-02"), EndDate: strptr("2026-12-31")},
			wantErr: "end date must not be before start date",
		},
		{
			// Only one bound set is legal - a trip can have a start and no
			// end - so there is nothing to compare against.
			name: "start only",
			req:  tripRequest{Title: "Iceland", StartDate: strptr("2026-08-20")},
		},
		{
			name: "end only",
			req:  tripRequest{Title: "Iceland", EndDate: strptr("2026-08-01")},
		},
		{
			name:    "malformed date still reported as a format error",
			req:     tripRequest{Title: "Iceland", StartDate: strptr("20-08-2026"), EndDate: strptr("2026-08-01")},
			wantErr: "dates must be in YYYY-MM-DD format",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate() = nil, want %q", c.wantErr)
			}
			if err.Error() != c.wantErr {
				t.Errorf("validate() = %q, want %q", err.Error(), c.wantErr)
			}
		})
	}
}
