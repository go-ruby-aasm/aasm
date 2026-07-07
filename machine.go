// Copyright (c) the go-ruby-aasm/aasm authors
//
// SPDX-License-Identifier: BSD-3-Clause

package aasm

// Callback is a state/event callback seam. It receives the arguments passed to
// Fire/FireBang and returns a value (mirroring a Ruby method result, ignored by
// the engine) and an error. A non-nil error stops the transition.
type Callback func(args []any) (any, error)

// Guard is a transition guard seam: it decides whether a transition may be
// taken. A false result blocks the transition (not an error); a non-nil error
// aborts the fire and is routed to the event's error callbacks.
type Guard func(args []any) (bool, error)

// ErrorCallback is the event `error` seam. It receives the error raised during a
// transition and may return nil to swallow it or an error to propagate.
type ErrorCallback func(err error, args []any) error

// stateDef is a state's definition inside a [Machine].
type stateDef struct {
	name    string
	initial bool
	final   bool
	enter   []Callback
	exit    []Callback
}

// transitionDef is a single from→to transition inside an event.
type transitionDef struct {
	from    []string
	to      string
	guards  []Guard
	unless  []Guard
	before  []Callback
	after   []Callback
	success []Callback
}

// eventDef is an event's definition inside a [Machine].
type eventDef struct {
	name        string
	transitions []*transitionDef
	before      []Callback
	after       []Callback
	success     []Callback
	errorCbs    []ErrorCallback
	afterCommit []Callback
}

// Machine is a state-machine definition — the pure-Go analogue of one `aasm do
// … end` block. It holds states and events but no per-object state; bind it to a
// host with [Machine.Bind]. A class with several `aasm(:name) do … end` blocks
// maps to several independent Machines.
type Machine struct {
	name        string
	whiny       bool
	initial     string
	stateOrder  []string
	stateByName map[string]*stateDef
	eventOrder  []string
	eventByName map[string]*eventDef
}

// New returns an empty machine with the given name (use "" for the gem's default
// unnamed machine). Whiny transitions are on by default, matching the gem.
func New(name string) *Machine {
	return &Machine{
		name:        name,
		whiny:       true,
		stateByName: map[string]*stateDef{},
		eventByName: map[string]*eventDef{},
	}
}

// Name reports the machine's name.
func (m *Machine) Name() string { return m.name }

// InitialState reports the state name flagged initial, or "" if none is.
func (m *Machine) InitialState() string { return m.initial }

// WhinyTransitions toggles whether a blocked/invalid Fire returns an error
// (true, the default) or (false, nil). It returns the machine for chaining.
func (m *Machine) WhinyTransitions(v bool) *Machine {
	m.whiny = v
	return m
}

// States returns the declared state names in declaration order.
func (m *Machine) States() []string { return append([]string(nil), m.stateOrder...) }

// Events returns the declared event names in declaration order.
func (m *Machine) Events() []string { return append([]string(nil), m.eventOrder...) }

// ensureState returns the state definition for name, creating it if a transition
// or declaration referenced a not-yet-seen state.
func (m *Machine) ensureState(name string) *stateDef {
	s := m.stateByName[name]
	if s == nil {
		s = &stateDef{name: name}
		m.stateByName[name] = s
		m.stateOrder = append(m.stateOrder, name)
	}
	return s
}

// StateOption configures a state in [Machine.State].
type StateOption func(*stateDef)

// Initial flags the state as the machine's initial state (the gem's
// `state :x, initial: true`).
func Initial() StateOption { return func(s *stateDef) { s.initial = true } }

// Final flags the state as a final state (the gem's `state :x, final: true`).
func Final() StateOption { return func(s *stateDef) { s.final = true } }

// Enter appends an enter callback, run when the machine enters the state.
func Enter(cb Callback) StateOption { return func(s *stateDef) { s.enter = append(s.enter, cb) } }

// Exit appends an exit callback, run when the machine leaves the state.
func Exit(cb Callback) StateOption { return func(s *stateDef) { s.exit = append(s.exit, cb) } }

// State declares (or extends) a state and returns the machine for chaining.
func (m *Machine) State(name string, opts ...StateOption) *Machine {
	s := m.ensureState(name)
	for _, o := range opts {
		o(s)
	}
	if s.initial {
		m.initial = name
	}
	return m
}

// EventOption configures an event in [Machine.Event].
type EventOption func(*eventDef)

// Before appends an event-level before callback.
func Before(cb Callback) EventOption { return func(e *eventDef) { e.before = append(e.before, cb) } }

// After appends an event-level after callback.
func After(cb Callback) EventOption { return func(e *eventDef) { e.after = append(e.after, cb) } }

// Success appends an event-level success callback (run after the transition and,
// for FireBang, after Persist).
func Success(cb Callback) EventOption { return func(e *eventDef) { e.success = append(e.success, cb) } }

// Error appends an event-level error callback.
func Error(cb ErrorCallback) EventOption {
	return func(e *eventDef) { e.errorCbs = append(e.errorCbs, cb) }
}

// AfterCommit appends an event-level after_commit callback, run by FireBang only
// after Persist and the success callbacks.
func AfterCommit(cb Callback) EventOption {
	return func(e *eventDef) { e.afterCommit = append(e.afterCommit, cb) }
}

// Transition is the specification of one from→to transition passed to
// [Transitions]. Guards must all pass and Unless guards must all fail for the
// transition to be selected.
type Transition struct {
	From    []string
	To      string
	Guards  []Guard
	Unless  []Guard
	Before  []Callback
	After   []Callback
	Success []Callback
}

// Transitions appends a transition to the event (the gem's `transitions from:,
// to:, guard:, …` inside an `event` block).
func Transitions(t Transition) EventOption {
	return func(e *eventDef) {
		e.transitions = append(e.transitions, &transitionDef{
			from:    t.From,
			to:      t.To,
			guards:  t.Guards,
			unless:  t.Unless,
			before:  t.Before,
			after:   t.After,
			success: t.Success,
		})
	}
}

// Event declares (or extends) an event and returns the machine for chaining.
// States named by the event's transitions are auto-registered if not already
// declared.
func (m *Machine) Event(name string, opts ...EventOption) *Machine {
	e := m.eventByName[name]
	if e == nil {
		e = &eventDef{name: name}
		m.eventByName[name] = e
		m.eventOrder = append(m.eventOrder, name)
	}
	for _, o := range opts {
		o(e)
	}
	for _, tr := range e.transitions {
		m.ensureState(tr.to)
		for _, f := range tr.from {
			m.ensureState(f)
		}
	}
	return m
}
