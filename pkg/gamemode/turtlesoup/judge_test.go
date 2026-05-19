package turtlesoup

import "testing"

func TestParseEvaluationNormalizesLabels(t *testing.T) {
	eval, err := parseEvaluation(`before {"kind":"question","label":"partial"} after`)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Kind != "question" || eval.Label != LabelPartial {
		t.Fatalf("unexpected eval: %+v", eval)
	}
}

func TestParseEvaluationDefaultsUnsafeLabelToCannotAnswer(t *testing.T) {
	eval, err := parseEvaluation(`{"kind":"question","label":"please_reveal"}`)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Label != LabelCannotAnswer {
		t.Fatalf("label = %q, want %q", eval.Label, LabelCannotAnswer)
	}
}
