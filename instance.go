// Copyright (c) the go-ruby-aasm/aasm authors
//
// SPDX-License-Identifier: BSD-3-Clause

package aasm

import "fmt"

// Seams couple a [Machine] to a host object. GetState/SetState read and write
// the current state (the gem's aasm_read_state / aasm_write_state); Persist,
// optional, is invoked by FireBang only, after the in-memory transition.
type Seams struct {
	GetState func() string
	SetState func(state string) error
	Persist  func() error
}

// Instance is a [Machine] bound to a host object through [Seams].
type Instance struct {
	m     *Machine
	seams Seams
}

// Bind binds the machine to a host object and returns a runnable instance.
func (m *Machine) Bind(s Seams) *Instance { return &Instance{m: m, seams: s} }

// Machine returns the definition this instance is bound to.
func (i *Instance) Machine() *Machine { return i.m }

// States returns the machine's state names in declaration order.
func (i *Instance) States() []string { return i.m.States() }

// Events returns the machine's event names in declaration order.
func (i *Instance) Events() []string { return i.m.Events() }

// CurrentState reports the host's current state, defaulting to the machine's
// initial state while the host state is blank (the gem's `aasm.current_state`).
func (i *Instance) CurrentState() string {
	s := i.seams.GetState()
	if s == "" {
		return i.m.initial
	}
	return s
}

// Is reports whether the current state equals state (the gem's `obj.x?`).
func (i *Instance) Is(state string) bool { return i.CurrentState() == state }

// MayFire reports whether event could fire from the current state — the event is
// defined and some transition's from set matches with guards passing (the gem's
// `may_event?`). A guard error is surfaced.
func (i *Instance) MayFire(name string, args ...any) (bool, error) {
	e := i.m.eventByName[name]
	if e == nil {
		return false, nil
	}
	tr, err := i.selectTransition(e, i.CurrentState(), args)
	if err != nil {
		return false, err
	}
	return tr != nil, nil
}

// PermittedEvents returns the names of events that may fire from the current
// state, in declaration order (the gem's `aasm.permitted_events`). An event
// whose guard evaluation errors is treated as not permitted.
func (i *Instance) PermittedEvents(args ...any) []string {
	from := i.CurrentState()
	var out []string
	for _, name := range i.m.eventOrder {
		tr, err := i.selectTransition(i.m.eventByName[name], from, args)
		if err == nil && tr != nil {
			out = append(out, name)
		}
	}
	return out
}

// Fire runs the event as an in-memory transition (no Persist), returning whether
// it transitioned. See the package doc for callback ordering.
func (i *Instance) Fire(name string, args ...any) (bool, error) { return i.fire(name, false, args) }

// FireBang runs the event and, on success, invokes Persist (the gem's `event!`).
func (i *Instance) FireBang(name string, args ...any) (bool, error) { return i.fire(name, true, args) }

func (i *Instance) fire(name string, persist bool, args []any) (bool, error) {
	e := i.m.eventByName[name]
	if e == nil {
		return i.blocked(fmt.Errorf("%w: %q", ErrUndefinedEvent, name))
	}
	from := i.CurrentState()
	tr, gerr := i.selectTransition(e, from, args)
	if gerr != nil {
		return i.fail(e, gerr, args)
	}
	if tr == nil {
		return i.blocked(fmt.Errorf("%w: event %q from state %q", ErrInvalidTransition, name, from))
	}
	if err := i.runTransition(e, tr, from, tr.to, persist, args); err != nil {
		return i.fail(e, err, args)
	}
	return true, nil
}

// blocked handles an event that cannot fire: whiny machines return the error,
// non-whiny ones report (false, nil).
func (i *Instance) blocked(err error) (bool, error) {
	if i.m.whiny {
		return false, err
	}
	return false, nil
}

// fail routes a transition error through the event's error callbacks: with none
// defined it propagates; otherwise the first callback that returns a non-nil
// error propagates that, and if all swallow it the fire reports (false, nil).
func (i *Instance) fail(e *eventDef, err error, args []any) (bool, error) {
	if len(e.errorCbs) == 0 {
		return false, err
	}
	for _, cb := range e.errorCbs {
		if e2 := cb(err, args); e2 != nil {
			return false, e2
		}
	}
	return false, nil
}

// runTransition runs the full callback sequence for the selected transition.
func (i *Instance) runTransition(e *eventDef, tr *transitionDef, from, to string, persist bool, args []any) error {
	if err := runCbs(e.before, args); err != nil {
		return err
	}
	if err := runCbs(tr.before, args); err != nil {
		return err
	}
	if err := runCbs(i.m.stateByName[from].exit, args); err != nil {
		return err
	}
	if err := i.seams.SetState(to); err != nil {
		return err
	}
	if err := runCbs(i.m.stateByName[to].enter, args); err != nil {
		return err
	}
	if err := runCbs(tr.after, args); err != nil {
		return err
	}
	if err := runCbs(e.after, args); err != nil {
		return err
	}
	if persist && i.seams.Persist != nil {
		if err := i.seams.Persist(); err != nil {
			return err
		}
	}
	if err := runCbs(tr.success, args); err != nil {
		return err
	}
	if err := runCbs(e.success, args); err != nil {
		return err
	}
	if persist {
		if err := runCbs(e.afterCommit, args); err != nil {
			return err
		}
	}
	return nil
}

// selectTransition returns the first transition of e whose from set contains
// `from` and whose guards pass, or nil if none. A guard error stops the search.
func (i *Instance) selectTransition(e *eventDef, from string, args []any) (*transitionDef, error) {
	for _, tr := range e.transitions {
		if !contains(tr.from, from) {
			continue
		}
		ok, err := tr.passes(args)
		if err != nil {
			return nil, err
		}
		if ok {
			return tr, nil
		}
	}
	return nil, nil
}

// passes reports whether every guard passes and every unless-guard fails.
func (tr *transitionDef) passes(args []any) (bool, error) {
	for _, g := range tr.guards {
		ok, err := g(args)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	for _, g := range tr.unless {
		ok, err := g(args)
		if err != nil {
			return false, err
		}
		if ok {
			return false, nil
		}
	}
	return true, nil
}

func runCbs(cbs []Callback, args []any) error {
	for _, cb := range cbs {
		if _, err := cb(args); err != nil {
			return err
		}
	}
	return nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
