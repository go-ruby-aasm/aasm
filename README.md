<p align="center"><img src="https://go-ruby-aasm.github.io/logo.png" alt="go-ruby-aasm/aasm" width="720"></p>

# aasm — go-ruby-aasm

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-aasm.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the deterministic core of Ruby's
[`aasm`](https://github.com/aasm/aasm) gem** — *Acts As State Machine*. It
reproduces the state-machine **definition** (states with `initial`/`final` flags
and `enter`/`exit` callbacks; events with `from → to` transitions, guards, and
`before`/`after`/`success`/`error`/`after_commit` callbacks) and the **runtime**
that fires an event against a host object — current state, the
`may_fire?`/`fire`/`fire!` trio, permitted events, whiny transitions, and the
faithful AASM callback ordering — **without any Ruby runtime**.

It is the AASM engine for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but a **standalone,
reusable** module.

> **What it is — and isn't.** Everything AASM does to *decide and sequence* a
> transition is deterministic and needs **no interpreter**, so it lives here as
> pure Go: selecting a transition by `from` set and guards, running the callback
> chain in AASM's documented order, changing state, and — for `fire!` —
> persisting. The three couplings to a host object are **seams**: reading and
> writing the current state (`GetState`/`SetState`), persistence (`Persist`, used
> by `fire!` only), and the guards/callbacks themselves (`func([]any) (bool,
> error)` / `func([]any) (any, error)`). A future rbgo binding wires those seams
> to Ruby methods and blocks; the engine calls no Ruby of its own.

## Features

Faithful port of the `aasm` gem's engine:

- **Definition** — `New(name)` then a fluent `State(…)` / `Event(…)` DSL mirroring
  `aasm do … end`. States carry `Initial()`, `Final()`, `Enter(cb)`, `Exit(cb)`;
  events carry `Before`, `After`, `Success`, `Error`, `AfterCommit`, and one or
  more `Transitions(Transition{From, To, Guards, Unless, Before, After,
  Success})`. States named only by a transition are auto-registered.
- **Instance** — `Bind(Seams)` couples a definition to a host object.
  `CurrentState` (defaults to the initial state until set), `Is(state)`,
  `MayFire(event)` (guards evaluated), `Fire(event, args…)` (in-memory) and
  `FireBang(event, args…)` (the gem's `event!`, persists), `States`, `Events`,
  `PermittedEvents`.
- **Callback ordering** — a successful fire runs: guards → event `before` →
  transition `before` → old state `exit` → **state change** → new state `enter` →
  transition `after` → event `after` → *(fire! only)* `Persist` → transition
  `success` → event `success` → *(fire! only)* `after_commit`.
- **Guards** — `Guards` (all must pass) and `Unless` (all must fail) select the
  transition. A false guard **blocks** (not an error); a guard error aborts the
  fire.
- **Whiny transitions** — on by default: a blocked/undefined fire returns
  `ErrInvalidTransition` / `ErrUndefinedEvent`; `WhinyTransitions(false)` makes it
  return `(false, nil)` instead.
- **Error routing** — a callback error is routed to the event's `Error` callbacks,
  which may swallow it (return nil) or re-raise; with none defined it propagates.
- **Multiple named machines** — several independent `Machine`s bind to distinct
  state fields on the same object (the gem's `aasm(:name) do … end`).

CGO-free, dependency-free (stdlib only), **100% test coverage**, `gofmt` +
`go vet` clean, and green across the six 64-bit Go targets (amd64, arm64,
riscv64, loong64, ppc64le, **s390x** — big-endian) plus `js/wasm` and
`wasip1/wasm`.

## Install

```sh
go get github.com/go-ruby-aasm/aasm
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/go-ruby-aasm/aasm"
)

// A host object owns its own state field; the engine reaches it through seams.
type job struct{ state string }

func main() {
	j := &job{}

	m := aasm.New("").
		State("sleeping", aasm.Initial()).
		State("running").
		State("cleaning").
		Event("run", aasm.Transitions(aasm.Transition{
			From: []string{"sleeping"}, To: "running",
		})).
		Event("clean", aasm.Transitions(aasm.Transition{
			From: []string{"running"}, To: "cleaning",
		}))

	inst := m.Bind(aasm.Seams{
		GetState: func() string { return j.state },
		SetState: func(s string) error { j.state = s; return nil },
		Persist:  func() error { /* db.Save(j) */ return nil },
	})

	fmt.Println(inst.CurrentState()) // sleeping (initial, before any set)

	ok, _ := inst.MayFire("run") // true
	_, _ = inst.Fire("run")      // sleeping -> running (in-memory)
	_, _ = inst.FireBang("clean") // running -> cleaning, then Persist

	fmt.Println(inst.CurrentState(), ok) // cleaning true
	fmt.Println(inst.PermittedEvents())  // events fireable from "cleaning"
}
```

### Guards, callbacks and error routing

```go
m := aasm.New("").
	State("pending", aasm.Initial()).
	State("active", aasm.Enter(func(a []any) (any, error) { /* on enter */ return nil, nil })).
	Event("activate",
		aasm.Before(func(a []any) (any, error) { return nil, nil }),
		aasm.Success(func(a []any) (any, error) { return nil, nil }),
		aasm.Error(func(err error, a []any) error { return err }), // re-raise (or return nil to swallow)
		aasm.Transitions(aasm.Transition{
			From:   []string{"pending"},
			To:     "active",
			Guards: []aasm.Guard{func(a []any) (bool, error) { return true, nil }},
		}),
	)
```

## Value model

| gem                                             | this package                                       |
| ----------------------------------------------- | -------------------------------------------------- |
| `aasm do … end`                                 | `New(name)` + `.State(…)` / `.Event(…)`            |
| `state :x, initial: true`                       | `State("x", Initial())`                            |
| `event :go do transitions from:, to: end`       | `Event("go", Transitions(Transition{From:, To:}))` |
| `before`/`after`/`success`/`error`/`after_commit` | `Before`/`After`/`Success`/`Error`/`AfterCommit`   |
| `before_enter`/`enter` · `before_exit`/`exit`   | `Enter(cb)` · `Exit(cb)`                           |
| `transitions … guard:/if:/unless:`              | `Transition{Guards: …, Unless: …}`                 |
| `obj.go`                                         | `inst.Fire("go", args…)`                           |
| `obj.go!`                                        | `inst.FireBang("go", args…)`                       |
| `obj.may_go?`                                    | `inst.MayFire("go", args…)`                        |
| `aasm.current_state`                            | `inst.CurrentState()`                              |
| `aasm.states` / `aasm.events`                   | `inst.States()` / `inst.Events()`                  |
| `aasm.permitted_events`                         | `inst.PermittedEvents()`                           |
| `aasm_read_state` / `aasm_write_state`          | `Seams.GetState` / `Seams.SetState`                |
| persisting `event!` save                        | `Seams.Persist` (host seam; `fire!` only)          |
| `AASM::InvalidTransition` / undefined event     | `ErrInvalidTransition` / `ErrUndefinedEvent`       |

## Tests & coverage

The suite is deterministic and self-contained: an in-memory host drives the
state/persist seams, and stub guards/callbacks drive every branch — guard-blocked
and invalid transitions, callback error propagation and swallowing, the persist
and set-state seam errors, whiny vs non-whiny, multiple named machines, and every
stage of the callback chain. Coverage holds at **100%** on every OS and arch lane.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## WebAssembly

Being pure Go (CGO=0), this library also compiles to **WebAssembly** — both
`GOOS=js GOARCH=wasm` (browser / Node.js) and `GOOS=wasip1 GOARCH=wasm` (WASI).
CI builds both targets on every push, alongside the six 64-bit native/qemu arches.

```sh
GOOS=js     GOARCH=wasm go build ./...   # browser / Node
GOOS=wasip1 GOARCH=wasm go build ./...   # WASI (wasmtime, wasmer, wasmedge, …)
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-aasm/aasm authors.
