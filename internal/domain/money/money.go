package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	ErrInvalidAmount    = errors.New("invalid monetary amount")
	ErrInvalidCurrency  = errors.New("invalid currency")
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrOverflow         = errors.New("monetary overflow")
)

// Money represents a monetary amount in minor units (cents).
type Money struct {
	minorUnits int64
	currency   string
}

// New creates a Money value from a decimal string.
// The amount must be non-negative and have exactly two decimal places.
func New(amount, currency string) (Money, error) {
	if !validCurrency(currency) {
		return Money{}, ErrInvalidCurrency
	}

	if amount == "" || strings.HasPrefix(amount, "-") ||
		strings.HasPrefix(amount, "+") {
		return Money{}, ErrInvalidAmount
	}

	parts := strings.Split(amount, ".")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) != 2 {
		return Money{}, ErrInvalidAmount
	}

	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return Money{}, ErrInvalidAmount
			}
		}
	}

	// "30.00" becomes "3000", avoiding floating-point arithmetic.
	digits := parts[0] + parts[1]
	units, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Money{}, ErrOverflow
	}

	return Money{
		minorUnits: units,
		currency:   currency,
	}, nil

}

// Zero creates a rero amount for a currency.
func Zero(currency string) (Money, error) {
	if !validCurrency(currency) {
		return Money{}, ErrInvalidCurrency
	}

	return Money{currency: currency}, nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}

	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return false
		}
	}

	return true
}

func (m Money) AmountInMinorUnits() int64 {
	return m.minorUnits
}

func (m Money) Currency() string {
	return m.currency
}

func (m Money) String() string {
	// Format without negating the value, wich could overflow at MinInt64.
	if m.minorUnits < 0 {
		whole := uint64(-(m.minorUnits + 1))
		whole++
		return fmt.Sprintf("-%d.%02d %s",
			whole/100, whole%100, m.currency)
	}

	return fmt.Sprintf("%d.%02d %s",
		m.minorUnits/100, m.minorUnits%100, m.currency)
}

func (m Money) Add(other Money) (Money, error) {
	if err := m.checkCurrency(other); err != nil {
		return Money{}, err
	}

	if (other.minorUnits > 0 && m.minorUnits > math.MaxInt64-other.minorUnits) ||
		(other.minorUnits < 0 && m.minorUnits < math.MinInt64-other.minorUnits) {
		return Money{}, ErrOverflow
	}

	return Money{
		minorUnits: m.minorUnits + other.minorUnits,
		currency:   m.currency,
	}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if err := m.checkCurrency(other); err != nil {
		return Money{}, err
	}

	// Subtracting a negative number can overflow at MaxInt64.
	if other.minorUnits < 0 &&
		m.minorUnits > math.MaxInt64+other.minorUnits {
		return Money{}, ErrOverflow
	}

	// Subtracting a positive number can overflow at MinInt64.
	if other.minorUnits > 0 &&
		m.minorUnits < math.MinInt64+other.minorUnits {
		return Money{}, ErrOverflow
	}

	return Money{
		minorUnits: m.minorUnits - other.minorUnits,
		currency:   m.currency,
	}, nil
}

func (m Money) Negate() (Money, error) {
	if m.minorUnits == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return Money{
		minorUnits: -m.minorUnits,
		currency:   m.currency,
	}, nil
}

// Comapre returns -1 when m < other, 0 when equal, and 1 when m > other.
func (m Money) Compare(other Money) (int, error) {
	if err := m.checkCurrency(other); err != nil {
		return 0, err
	}

	switch {
	case m.minorUnits < other.minorUnits:
		return -1, nil
	case m.minorUnits > other.minorUnits:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) checkCurrency(other Money) error {
	if m.currency == "" || other.currency == "" {
		return ErrInvalidCurrency
	}

	if m.currency != other.currency {
		return ErrCurrencyMismatch
	}

	return nil
}
