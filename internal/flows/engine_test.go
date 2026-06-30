package flows_test

import (
	"context"
	"testing"

	"deutsch-helper/internal/flows"
)

func TestEngineGetSetClear(t *testing.T) {
	ctx := context.Background()
	engine := flows.NewEngine(flows.NewInMemoryStorage())

	t.Run("nil when no state", func(t *testing.T) {
		st, err := engine.GetState(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if st != nil {
			t.Fatalf("expected nil, got %+v", st)
		}
	})

	t.Run("set and get", func(t *testing.T) {
		st := flows.NewUserState(42, flows.FlowVoice, flows.StateVoiceDraft)
		st.Payload[flows.PayloadDraftText] = "hello"

		if err := engine.SetState(ctx, st); err != nil {
			t.Fatal(err)
		}

		got, err := engine.GetState(ctx, 42)
		if err != nil {
			t.Fatal(err)
		}
		if got.Flow != flows.FlowVoice {
			t.Fatalf("flow: want %q, got %q", flows.FlowVoice, got.Flow)
		}
		if got.State != flows.StateVoiceDraft {
			t.Fatalf("state: want %q, got %q", flows.StateVoiceDraft, got.State)
		}
		if got.Payload[flows.PayloadDraftText] != "hello" {
			t.Fatalf("payload draft_text: want %q, got %v", "hello", got.Payload[flows.PayloadDraftText])
		}
	})

	t.Run("clear removes state", func(t *testing.T) {
		if err := engine.ClearState(ctx, 42); err != nil {
			t.Fatal(err)
		}
		st, err := engine.GetState(ctx, 42)
		if err != nil {
			t.Fatal(err)
		}
		if st != nil {
			t.Fatalf("expected nil after clear, got %+v", st)
		}
	})
}

func TestEngineIsInFlow(t *testing.T) {
	ctx := context.Background()
	engine := flows.NewEngine(flows.NewInMemoryStorage())

	cases := []struct {
		name      string
		setup     func()
		userID    int64
		flow      flows.FlowName
		wantIn    bool
	}{
		{
			name:   "no state → not in flow",
			setup:  func() {},
			userID: 1,
			flow:   flows.FlowVoice,
			wantIn: false,
		},
		{
			name: "in voice flow",
			setup: func() {
				_ = engine.SetState(ctx, flows.NewUserState(2, flows.FlowVoice, flows.StateVoiceDraft))
			},
			userID: 2,
			flow:   flows.FlowVoice,
			wantIn: true,
		},
		{
			name: "different flow → not in voice flow",
			setup: func() {
				_ = engine.SetState(ctx, flows.NewUserState(3, "other", "step1"))
			},
			userID: 3,
			flow:   flows.FlowVoice,
			wantIn: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			got, err := engine.IsInFlow(ctx, tc.userID, tc.flow)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantIn {
				t.Fatalf("IsInFlow = %v, want %v", got, tc.wantIn)
			}
		})
	}
}

func TestEngineIsInState(t *testing.T) {
	ctx := context.Background()
	engine := flows.NewEngine(flows.NewInMemoryStorage())
	_ = engine.SetState(ctx, flows.NewUserState(10, flows.FlowVoice, flows.StateVoiceEdit))

	cases := []struct {
		name   string
		flow   flows.FlowName
		state  flows.StateName
		wantIn bool
	}{
		{"exact match", flows.FlowVoice, flows.StateVoiceEdit, true},
		{"wrong state", flows.FlowVoice, flows.StateVoiceDraft, false},
		{"wrong flow", "other", flows.StateVoiceEdit, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.IsInState(ctx, 10, tc.flow, tc.state)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantIn {
				t.Fatalf("IsInState = %v, want %v", got, tc.wantIn)
			}
		})
	}
}

func TestInMemoryStorageIsolation(t *testing.T) {
	ctx := context.Background()
	store := flows.NewInMemoryStorage()

	original := flows.NewUserState(1, flows.FlowVoice, flows.StateVoiceDraft)
	original.Payload["key"] = "original"
	_ = store.Set(ctx, original)

	got, _ := store.Get(ctx, 1)
	got.Payload["key"] = "mutated"

	got2, _ := store.Get(ctx, 1)
	if got2.Payload["key"] != "original" {
		t.Fatalf("storage is not isolated: got %q", got2.Payload["key"])
	}
}
