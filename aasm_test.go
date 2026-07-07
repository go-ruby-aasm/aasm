// Copyright (c) the go-ruby-aasm/aasm authors
//
// SPDX-License-Identifier: BSD-3-Clause

package aasm_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-ruby-aasm/aasm"
)

// host is a tiny in-memory object exercising the state/persist seams.
type host struct {
	state     string
	saves     int
	saveErr   error
	setErr    error
	setCalled int
}

func (h *host) seams() aasm.Seams {
	return aasm.Seams{
		GetState: func() string { return h.state },
		SetState: func(s string) error {
			h.setCalled++
			if h.setErr != nil {
				return h.setErr
			}
			h.state = s
			return nil
		},
		Persist: func() error {
			h.saves++
			return h.saveErr
		},
	}
}

var errBoom = errors.New("boom")

func okCb(args []any) (any, error)  { return nil, nil }
func errCb(args []any) (any, error) { return nil, errBoom }

func passGuard(args []any) (bool, error)  { return true, nil }
func blockGuard(args []any) (bool, error) { return false, nil }
func errGuard(args []any) (bool, error)   { return false, errBoom }

// job builds the canonical sleeping/running/cleaning machine.
func job() *aasm.Machine {
	return aasm.New("").
		State("sleeping", aasm.Initial()).
		State("running").
		State("cleaning", aasm.Final()).
		Event("run", aasm.Transitions(aasm.Transition{From: []string{"sleeping"}, To: "running"})).
		Event("clean", aasm.Transitions(aasm.Transition{From: []string{"running"}, To: "cleaning"})).
		Event("sleep", aasm.Transitions(aasm.Transition{From: []string{"running", "cleaning"}, To: "sleeping"}))
}

func TestHappyPathFireAndState(t *testing.T) {
	h := &host{}
	inst := job().Bind(h.seams())

	if got := inst.CurrentState(); got != "sleeping" {
		t.Fatalf("initial current_state = %q, want sleeping", got)
	}
	if !inst.Is("sleeping") || inst.Is("running") {
		t.Fatalf("Is() wrong at start")
	}

	ok, err := inst.Fire("run")
	if !ok || err != nil {
		t.Fatalf("Fire(run) = %v,%v", ok, err)
	}
	if inst.CurrentState() != "running" || h.state != "running" {
		t.Fatalf("state after run = %q", inst.CurrentState())
	}
	if h.saves != 0 {
		t.Fatalf("Fire must not persist, saves=%d", h.saves)
	}
}

func TestFireBangPersists(t *testing.T) {
	h := &host{}
	inst := job().Bind(h.seams())
	ok, err := inst.FireBang("run")
	if !ok || err != nil {
		t.Fatalf("FireBang(run) = %v,%v", ok, err)
	}
	if h.saves != 1 {
		t.Fatalf("FireBang must persist once, saves=%d", h.saves)
	}
}

func TestFireBangNilPersistSeam(t *testing.T) {
	h := &host{}
	s := h.seams()
	s.Persist = nil // FireBang must tolerate a missing persist seam
	inst := job().Bind(s)
	ok, err := inst.FireBang("run")
	if !ok || err != nil {
		t.Fatalf("FireBang with nil Persist = %v,%v", ok, err)
	}
	if inst.CurrentState() != "running" {
		t.Fatalf("state = %q", inst.CurrentState())
	}
}

func TestPersistSeamError(t *testing.T) {
	h := &host{saveErr: errBoom}
	inst := job().Bind(h.seams())
	ok, err := inst.FireBang("run")
	if ok || !errors.Is(err, errBoom) {
		t.Fatalf("FireBang persist error = %v,%v", ok, err)
	}
}

func TestSetStateSeamError(t *testing.T) {
	h := &host{setErr: errBoom}
	inst := job().Bind(h.seams())
	ok, err := inst.Fire("run")
	if ok || !errors.Is(err, errBoom) {
		t.Fatalf("Fire with SetState error = %v,%v", ok, err)
	}
}

func TestInvalidEventWhiny(t *testing.T) {
	h := &host{}
	inst := job().Bind(h.seams())
	ok, err := inst.Fire("nope")
	if ok || !errors.Is(err, aasm.ErrUndefinedEvent) {
		t.Fatalf("undefined event = %v,%v", ok, err)
	}
}

func TestInvalidEventNonWhiny(t *testing.T) {
	h := &host{}
	inst := job().WhinyTransitions(false).Bind(h.seams())
	ok, err := inst.Fire("nope")
	if ok || err != nil {
		t.Fatalf("non-whiny undefined event = %v,%v", ok, err)
	}
}

func TestInvalidTransitionFromState(t *testing.T) {
	h := &host{}
	inst := job().Bind(h.seams())
	// clean is only valid from running; from sleeping it is invalid.
	ok, err := inst.Fire("clean")
	if ok || !errors.Is(err, aasm.ErrInvalidTransition) {
		t.Fatalf("invalid transition = %v,%v", ok, err)
	}
}

func TestGuardBlockedWhinyAndNonWhiny(t *testing.T) {
	build := func(whiny bool) *aasm.Instance {
		h := &host{}
		m := aasm.New("").
			State("a", aasm.Initial()).
			State("b").
			Event("go", aasm.Transitions(aasm.Transition{
				From:   []string{"a"},
				To:     "b",
				Guards: []aasm.Guard{blockGuard},
			}))
		if !whiny {
			m.WhinyTransitions(false)
		}
		return m.Bind(h.seams())
	}

	ok, err := build(true).Fire("go")
	if ok || !errors.Is(err, aasm.ErrInvalidTransition) {
		t.Fatalf("guard-blocked whiny = %v,%v", ok, err)
	}
	ok, err = build(false).Fire("go")
	if ok || err != nil {
		t.Fatalf("guard-blocked non-whiny = %v,%v", ok, err)
	}
}

func TestGuardAndUnlessSelection(t *testing.T) {
	h := &host{}
	inst := aasm.New("").
		State("a", aasm.Initial()).
		State("b").State("c").
		// First transition blocked by an unless-guard that fires; second passes.
		Event("go",
			aasm.Transitions(aasm.Transition{
				From:   []string{"a"},
				To:     "b",
				Unless: []aasm.Guard{passGuard}, // unless(true) => blocked
			}),
			aasm.Transitions(aasm.Transition{
				From:   []string{"a"},
				To:     "c",
				Guards: []aasm.Guard{passGuard},
				Unless: []aasm.Guard{blockGuard}, // unless(false) => allowed
			}),
		).Bind(h.seams())

	ok, err := inst.Fire("go")
	if !ok || err != nil {
		t.Fatalf("Fire(go) = %v,%v", ok, err)
	}
	if inst.CurrentState() != "c" {
		t.Fatalf("selected wrong transition, state=%q", inst.CurrentState())
	}
}

func TestGuardErrorPropagates(t *testing.T) {
	h := &host{}
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go", aasm.Transitions(aasm.Transition{
			From: []string{"a"}, To: "b", Guards: []aasm.Guard{errGuard},
		})).Bind(h.seams())
	ok, err := inst.Fire("go")
	if ok || !errors.Is(err, errBoom) {
		t.Fatalf("guard error = %v,%v", ok, err)
	}
}

func TestUnlessErrorPropagates(t *testing.T) {
	h := &host{}
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go", aasm.Transitions(aasm.Transition{
			From: []string{"a"}, To: "b", Unless: []aasm.Guard{errGuard},
		})).Bind(h.seams())
	ok, err := inst.Fire("go")
	if ok || !errors.Is(err, errBoom) {
		t.Fatalf("unless error = %v,%v", ok, err)
	}
}

func TestMayFire(t *testing.T) {
	h := &host{}
	inst := job().Bind(h.seams())

	// defined + permitted
	ok, err := inst.MayFire("run")
	if !ok || err != nil {
		t.Fatalf("MayFire(run) = %v,%v", ok, err)
	}
	// defined but not from this state
	ok, err = inst.MayFire("clean")
	if ok || err != nil {
		t.Fatalf("MayFire(clean) = %v,%v", ok, err)
	}
	// undefined event
	ok, err = inst.MayFire("nope")
	if ok || err != nil {
		t.Fatalf("MayFire(nope) = %v,%v", ok, err)
	}
}

func TestMayFireGuardError(t *testing.T) {
	h := &host{}
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go", aasm.Transitions(aasm.Transition{
			From: []string{"a"}, To: "b", Guards: []aasm.Guard{errGuard},
		})).Bind(h.seams())
	ok, err := inst.MayFire("go")
	if ok || !errors.Is(err, errBoom) {
		t.Fatalf("MayFire guard error = %v,%v", ok, err)
	}
}

func TestPermittedEvents(t *testing.T) {
	h := &host{}
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("good", aasm.Transitions(aasm.Transition{From: []string{"a"}, To: "b"})).
		Event("elsewhere", aasm.Transitions(aasm.Transition{From: []string{"b"}, To: "a"})).
		Event("bad", aasm.Transitions(aasm.Transition{
			From: []string{"a"}, To: "b", Guards: []aasm.Guard{errGuard},
		})).
		Bind(h.seams())

	got := inst.PermittedEvents()
	want := []string{"good"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PermittedEvents = %v, want %v", got, want)
	}
}

func TestCallbackOrdering(t *testing.T) {
	h := &host{}
	var log []string
	rec := func(tag string) aasm.Callback {
		return func(args []any) (any, error) { log = append(log, tag); return nil, nil }
	}

	inst := aasm.New("").
		State("a", aasm.Initial(), aasm.Exit(rec("a.exit")), aasm.Enter(rec("a.enter"))).
		State("b", aasm.Enter(rec("b.enter")), aasm.Exit(rec("b.exit"))).
		Event("go",
			aasm.Before(rec("ev.before")),
			aasm.After(rec("ev.after")),
			aasm.Success(rec("ev.success")),
			aasm.AfterCommit(rec("ev.after_commit")),
			aasm.Transitions(aasm.Transition{
				From:    []string{"a"},
				To:      "b",
				Before:  []aasm.Callback{rec("tr.before")},
				After:   []aasm.Callback{rec("tr.after")},
				Success: []aasm.Callback{rec("tr.success")},
			}),
		).Bind(h.seams())

	if _, err := inst.FireBang("go"); err != nil {
		t.Fatalf("FireBang(go): %v", err)
	}
	want := []string{
		"ev.before", "tr.before", "a.exit", "b.enter",
		"tr.after", "ev.after", "tr.success", "ev.success", "ev.after_commit",
	}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("callback order:\n got %v\nwant %v", log, want)
	}
}

func TestFireNoAfterCommit(t *testing.T) {
	h := &host{}
	var committed bool
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go",
			aasm.AfterCommit(func(args []any) (any, error) { committed = true; return nil, nil }),
			aasm.Transitions(aasm.Transition{From: []string{"a"}, To: "b"}),
		).Bind(h.seams())
	if _, err := inst.Fire("go"); err != nil {
		t.Fatalf("Fire(go): %v", err)
	}
	if committed {
		t.Fatalf("after_commit must not run for non-bang Fire")
	}
}

// TestEachCallbackStageError injects an error at every stage of the transition
// sequence and asserts it propagates.
func TestEachCallbackStageError(t *testing.T) {
	stages := []string{
		"ev.before", "tr.before", "state.exit", "setstate", "state.enter",
		"tr.after", "ev.after", "persist", "tr.success", "ev.success", "ev.after_commit",
	}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			h := &host{}
			if stage == "setstate" {
				h.setErr = errBoom
			}
			if stage == "persist" {
				h.saveErr = errBoom
			}

			pick := func(want string) aasm.Callback {
				if stage == want {
					return errCb
				}
				return okCb
			}

			inst := aasm.New("").
				State("a", aasm.Initial(), aasm.Exit(pick("state.exit"))).
				State("b", aasm.Enter(pick("state.enter"))).
				Event("go",
					aasm.Before(pick("ev.before")),
					aasm.After(pick("ev.after")),
					aasm.Success(pick("ev.success")),
					aasm.AfterCommit(pick("ev.after_commit")),
					aasm.Transitions(aasm.Transition{
						From:    []string{"a"},
						To:      "b",
						Before:  []aasm.Callback{pick("tr.before")},
						After:   []aasm.Callback{pick("tr.after")},
						Success: []aasm.Callback{pick("tr.success")},
					}),
				).Bind(h.seams())

			ok, err := inst.FireBang("go")
			if ok || !errors.Is(err, errBoom) {
				t.Fatalf("stage %s: FireBang = %v,%v", stage, ok, err)
			}
		})
	}
}

func TestErrorCallbackSwallows(t *testing.T) {
	h := &host{}
	var seen error
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go",
			aasm.Before(errCb),
			aasm.Error(func(err error, args []any) error { seen = err; return nil }), // swallow
			aasm.Transitions(aasm.Transition{From: []string{"a"}, To: "b"}),
		).Bind(h.seams())
	ok, err := inst.Fire("go")
	if ok || err != nil {
		t.Fatalf("swallowing error cb = %v,%v", ok, err)
	}
	if !errors.Is(seen, errBoom) {
		t.Fatalf("error callback saw %v", seen)
	}
}

func TestErrorCallbackReraises(t *testing.T) {
	h := &host{}
	custom := errors.New("wrapped")
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go",
			aasm.Before(errCb),
			aasm.Error(func(err error, args []any) error { return custom }), // re-raise
			aasm.Transitions(aasm.Transition{From: []string{"a"}, To: "b"}),
		).Bind(h.seams())
	ok, err := inst.Fire("go")
	if ok || !errors.Is(err, custom) {
		t.Fatalf("re-raising error cb = %v,%v", ok, err)
	}
}

func TestArgsThreadThrough(t *testing.T) {
	h := &host{}
	var gotGuard, gotCb []any
	inst := aasm.New("").
		State("a", aasm.Initial()).State("b").
		Event("go",
			aasm.Before(func(args []any) (any, error) { gotCb = args; return nil, nil }),
			aasm.Transitions(aasm.Transition{
				From: []string{"a"}, To: "b",
				Guards: []aasm.Guard{func(args []any) (bool, error) { gotGuard = args; return true, nil }},
			}),
		).Bind(h.seams())
	if _, err := inst.Fire("go", 1, "two"); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	want := []any{1, "two"}
	if !reflect.DeepEqual(gotGuard, want) || !reflect.DeepEqual(gotCb, want) {
		t.Fatalf("args threading: guard=%v cb=%v", gotGuard, gotCb)
	}
}

func TestMultipleNamedMachines(t *testing.T) {
	// Two independent machines bound to distinct state fields on one object.
	type doc struct{ workflow, review string }
	d := &doc{}

	wf := aasm.New("workflow").
		State("draft", aasm.Initial()).State("published").
		Event("publish", aasm.Transitions(aasm.Transition{From: []string{"draft"}, To: "published"}))
	rv := aasm.New("review").
		State("pending", aasm.Initial()).State("approved").
		Event("approve", aasm.Transitions(aasm.Transition{From: []string{"pending"}, To: "approved"}))

	wfi := wf.Bind(aasm.Seams{
		GetState: func() string { return d.workflow },
		SetState: func(s string) error { d.workflow = s; return nil },
	})
	rvi := rv.Bind(aasm.Seams{
		GetState: func() string { return d.review },
		SetState: func(s string) error { d.review = s; return nil },
	})

	if wfi.Machine().Name() != "workflow" || rvi.Machine().Name() != "review" {
		t.Fatalf("machine names wrong")
	}
	if _, err := wfi.Fire("publish"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if d.workflow != "published" || d.review != "" || rvi.CurrentState() != "pending" {
		t.Fatalf("machines not independent: %+v", d)
	}
	if _, err := rvi.Fire("approve"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if wfi.CurrentState() != "published" || rvi.CurrentState() != "approved" {
		t.Fatalf("final: %+v", d)
	}
}

func TestDefinitionAccessors(t *testing.T) {
	m := job()
	if m.Name() != "" {
		t.Fatalf("name = %q", m.Name())
	}
	if m.InitialState() != "sleeping" {
		t.Fatalf("initial = %q", m.InitialState())
	}
	if got, want := m.States(), []string{"sleeping", "running", "cleaning"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("States = %v, want %v", got, want)
	}
	if got, want := m.Events(), []string{"run", "clean", "sleep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Events = %v, want %v", got, want)
	}

	h := &host{}
	inst := m.Bind(h.seams())
	if !reflect.DeepEqual(inst.States(), m.States()) {
		t.Fatalf("Instance.States mismatch")
	}
	if !reflect.DeepEqual(inst.Events(), m.Events()) {
		t.Fatalf("Instance.Events mismatch")
	}
}

func TestRedeclareStateAndEvent(t *testing.T) {
	h := &host{}
	// State "a" auto-registered by the event's transition, then re-declared as
	// initial; Event "go" declared twice to add a second transition.
	m := aasm.New("").
		Event("go", aasm.Transitions(aasm.Transition{From: []string{"a"}, To: "b"})).
		State("a", aasm.Initial()).
		Event("go", aasm.Transitions(aasm.Transition{From: []string{"b"}, To: "a"}))

	if m.InitialState() != "a" {
		t.Fatalf("initial = %q", m.InitialState())
	}
	inst := m.Bind(h.seams())
	if _, err := inst.Fire("go"); err != nil { // a -> b
		t.Fatalf("go1: %v", err)
	}
	if inst.CurrentState() != "b" {
		t.Fatalf("state = %q", inst.CurrentState())
	}
	if _, err := inst.Fire("go"); err != nil { // b -> a (second transition)
		t.Fatalf("go2: %v", err)
	}
	if inst.CurrentState() != "a" {
		t.Fatalf("state = %q", inst.CurrentState())
	}
}

func TestNonBlankCurrentState(t *testing.T) {
	h := &host{state: "running"} // host already past initial
	inst := job().Bind(h.seams())
	if inst.CurrentState() != "running" {
		t.Fatalf("current = %q", inst.CurrentState())
	}
}
