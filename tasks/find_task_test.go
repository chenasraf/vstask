package tasks

import (
	"strings"
	"testing"
)

func TestFindTask_ExactMatch(t *testing.T) {
	taskList := []Task{
		{Label: "🚀 Deploy"},
		{Label: "build"},
		{Label: "test"},
	}
	got, err := FindTask(taskList, "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Label != "build" {
		t.Fatalf("got %q, want %q", got.Label, "build")
	}
}

func TestFindTask_ExactMatchTakesPrecedence(t *testing.T) {
	taskList := []Task{
		{Label: "build-all"},
		{Label: "build"},
	}
	got, err := FindTask(taskList, "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Label != "build" {
		t.Fatalf("exact match should win: got %q, want %q", got.Label, "build")
	}
}

func TestFindTask_PartialMatch(t *testing.T) {
	taskList := []Task{
		{Label: "🚀 Deploy"},
		{Label: "build"},
		{Label: "test"},
	}
	got, err := FindTask(taskList, "deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Label != "🚀 Deploy" {
		t.Fatalf("got %q, want %q", got.Label, "🚀 Deploy")
	}
}

func TestFindTask_CaseInsensitive(t *testing.T) {
	taskList := []Task{
		{Label: "Run Tests"},
	}
	got, err := FindTask(taskList, "run tests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Label != "Run Tests" {
		t.Fatalf("got %q, want %q", got.Label, "Run Tests")
	}
}

func TestFindTask_NoMatch(t *testing.T) {
	taskList := []Task{
		{Label: "build"},
		{Label: "test"},
	}
	_, err := FindTask(taskList, "deploy")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestFindTask_MultipleMatches(t *testing.T) {
	taskList := []Task{
		{Label: "🚀 deploy staging"},
		{Label: "🚀 deploy prod"},
		{Label: "build"},
	}
	_, err := FindTask(taskList, "deploy")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple tasks match") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "deploy staging") {
		t.Fatalf("error should list matching tasks: %v", err)
	}
	if !strings.Contains(err.Error(), "deploy prod") {
		t.Fatalf("error should list matching tasks: %v", err)
	}
}

func TestFindTask_EmptyList(t *testing.T) {
	_, err := FindTask(nil, "anything")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
