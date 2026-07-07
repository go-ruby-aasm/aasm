// Copyright (c) the go-ruby-aasm/aasm authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package aasm is a pure-Go (CGO-free) reimplementation of the deterministic
// core of Ruby's [aasm] gem — "Acts As State Machine". It reproduces the state
// machine DEFINITION (states with initial/final flags and enter/exit callbacks;
// events with from→to transitions, guards, and before/after/success/error/
// after_commit callbacks) and the runtime that fires an event against a host
// object — the current state, the may_fire?/fire/fire! trio, permitted events,
// whiny transitions, and the faithful AASM callback ordering — without any Ruby
// runtime.
//
// It is the AASM engine for
// [go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but a
// standalone, reusable module.
//
// # Definition and instance
//
// A [Machine] is a state-machine definition, built with a small fluent DSL that
// mirrors the gem's `aasm do … end` block:
//
//	m := aasm.New("").
//		State("sleeping", aasm.Initial()).
//		State("running").
//		Event("run", aasm.Transitions(aasm.Transition{
//			From: []string{"sleeping"}, To: "running",
//		}))
//
// A [Machine] carries no per-object state. It is bound to a host object through
// [Seams] to yield an [Instance]:
//
//	inst := m.Bind(aasm.Seams{
//		GetState: func() string { return obj.state },
//		SetState: func(s string) error { obj.state = s; return nil },
//		Persist:  func() error { return db.Save(obj) },
//	})
//
//	inst.CurrentState()          // "sleeping" (the initial state until set)
//	ok, err := inst.MayFire("run")
//	ok, err = inst.Fire("run")   // in-memory transition
//	ok, err = inst.FireBang("run") // transition + Persist (the gem's `run!`)
//
// # Seams
//
// The three host couplings are seams so the same engine drives a plain struct,
// an ORM row, or a Ruby object:
//
//   - GetState / SetState read and write the current state on the host object
//     (the gem's aasm_read_state / aasm_write_state).
//   - Persist is invoked by FireBang only, after the in-memory transition, to
//     persist the object (the gem's `event!` save). Fire never persists.
//
// Guards and callbacks are themselves seams — [Guard] is
// func([]any) (bool, error) and [Callback] is func([]any) (any, error) — that a
// binding wires to Ruby methods or blocks; the `any` return mirrors a Ruby
// method result and is ignored by the engine.
//
// # Callback ordering
//
// A successful fire runs, in order: the transition guards (to select the
// transition), the event `before`, the transition `before`, the old state's
// `exit`, the state change, the new state's `enter`, the transition `after`, the
// event `after`, then — for FireBang — Persist, then the transition and event
// `success`, and finally — for FireBang — `after_commit`. Any callback error
// stops the sequence and is routed to the event's `error` callbacks (which may
// swallow or re-raise); if none are defined it propagates. A guard that returns
// false is not an error: it blocks the transition, which raises
// [ErrInvalidTransition] when whiny (the default) or returns (false, nil) when
// [Machine.WhinyTransitions] is false.
//
// [aasm]: https://github.com/aasm/aasm
package aasm
