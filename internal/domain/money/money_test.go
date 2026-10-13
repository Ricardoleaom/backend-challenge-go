package money

import (
	"errors"
	"math"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		amount   string
		currency string
		want     int64
		wantErr  error
	}{
		{
			name:     "valid amount",
			amount:   "25.00",
			currency: "BRL",
			want:     2500,
		},
		{
			name:     "zero amount",
			amount:   "0.00",
			currency: "BRL",
			want:     0,
		},
		{
			name:     "reject negative amount",
			amount:   "-1.00",
			currency: "BRL",
			wantErr:  ErrInvalidAmount,
		},
		{
			name:     "reject scientific notation",
			amount:   "1e3",
			currency: "BRL",
			wantErr:  ErrInvalidAmount,
		},
		{
			name:     "reject excess decimal places",
			amount:   "1.999",
			currency: "BRL",
			wantErr:  ErrInvalidAmount,
		},
		{
			name:     "reject missing decimal places",
			amount:   "25",
			currency: "BRL",
			wantErr:  ErrInvalidAmount,
		},
		{
			name:     "reject invalid currency",
			amount:   "25",
			currency: "brl",
			wantErr:  ErrInvalidCurrency,
		},
		{
			name:     "reject overflow",
			amount:   "92233720368547758.08",
			currency: "BRL",
			wantErr:  ErrOverflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.amount, tt.currency)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if err != nil {
				return
			}

			if got.AmountInMinorUnits() != tt.want {
				t.Errorf("expected %d minor units, got %d",
					tt.want, got.AmountInMinorUnits())
			}
		})
	}
}

func TestAddAndSubtract(t *testing.T) {
	first, _ := New("25.50", "BRL")
	second, _ := New("10.25", "BRL")

	sum, err := first.Add(second)
	if err != nil {
		t.Fatal(err)
	}
	if sum.AmountInMinorUnits() != 3575 {
		t.Errorf("expected 3575, got %d", sum.AmountInMinorUnits())
	}

	difference, err := first.Subtract(second)
	if err != nil {
		t.Fatal(err)
	}

	if difference.AmountInMinorUnits() != 1525 {
		t.Errorf("expected 1525, got %d", difference.AmountInMinorUnits())
	}
}

func TestSubtractCanProduceNegativeValue(t *testing.T) {
	first, _ := New("10.00", "BRL")
	second, _ := New("25.00", "BRL")

	got, err := first.Subtract(second)
	if err != nil {
		t.Fatal(err)
	}

	if got.AmountInMinorUnits() != -1500 {
		t.Errorf("expected -1500, got %d", got.AmountInMinorUnits())
	}
}

func TestCurrencyMismatch(t *testing.T) {
	brl, _ := New("10.00", "BRL")
	usd, _ := New("10.00", "USD")

	_, err := brl.Add(usd)
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("expected currency mismatch, got %v", err)
	}
}

func TestCompare(t *testing.T) {
	first, _ := New("10.00", "BRL")
	second, _ := New("20.00", "BRL")

	got, err := first.Compare(second)
	if err != nil {
		t.Fatal(err)
	}
	if got != -1 {
		t.Errorf("expected -1, got %d", got)
	}
}

func TestNegateAndOverflow(t *testing.T) {
	value, _ := New("10.00", "BRL")

	negative, err := value.Negate()
	if err != nil {
		t.Fatal(err)
	}
	if negative.AmountInMinorUnits() != -1000 {
		t.Errorf("expected -1000, got %d", negative.AmountInMinorUnits())
	}

	minimum := Money{minorUnits: math.MinInt64, currency: "BRL"}
	if _, err := minimum.Negate(); !errors.Is(err, ErrOverflow) {
		t.Errorf("expected overflow, got %v", err)
	}
}
