package operation

import (
	"context"
	"testing"
	"time"

	"github.com/brilliantkid87/rop/pkg/rop"
)

func TestValidateRejectsUnknownVocabulary(t *testing.T) {
	cases := []Operation{
		{ID: "x", Reversibility: "SOMETHING_ELSE", Guarantee: rop.GuaranteeEXACT},
		{ID: "x", Reversibility: rop.ReversibilityREVERSIBLE, Guarantee: "SORT_OF"},
		{ID: "x", Reversibility: rop.ReversibilityIRREVERSIBLE, Guarantee: rop.GuaranteeNONE, TTL: time.Hour},
		{ID: "x", Reversibility: rop.ReversibilityIRREVERSIBLE, Guarantee: rop.GuaranteeNONE, ReverseFunc: func(context.Context, ReverseInput) (ReverseOutput, error) { return ReverseOutput{}, nil }},
		{ID: "", Reversibility: rop.ReversibilityREVERSIBLE, Guarantee: rop.GuaranteeEXACT},
		{ID: "x", Reversibility: rop.ReversibilityREVERSIBLE, Guarantee: rop.GuaranteeEXACT, ReverseFunc: func(context.Context, ReverseInput) (ReverseOutput, error) { return ReverseOutput{}, nil }},
	}
	for i, o := range cases {
		if err := o.Validate(); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestNewRegistryRejectsDuplicates(t *testing.T) {
	ok := Operation{ID: "a", Reversibility: rop.ReversibilityREVERSIBLE, Guarantee: rop.GuaranteeEXACT}
	if _, err := NewRegistry(ok, ok); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestRegistryGet(t *testing.T) {
	reg, err := NewRegistry(Operation{ID: "a", Reversibility: rop.ReversibilityREVERSIBLE, Guarantee: rop.GuaranteeEXACT})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("a"); !ok {
		t.Error("expected registered operation")
	}
	if _, ok := reg.Get("b"); ok {
		t.Error("unexpected operation")
	}
}
