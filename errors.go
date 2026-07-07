// Copyright (c) the go-ruby-aasm/aasm authors
//
// SPDX-License-Identifier: BSD-3-Clause

package aasm

import "errors"

// The gem's transition errors, exposed as sentinels so a host (rbgo) can map
// them onto the Ruby AASM exception tree and callers can match with errors.Is.
var (
	// ErrUndefinedEvent is returned (when whiny) by Fire / FireBang for an event
	// the machine does not define — the gem raises AASM::UndefinedState /
	// NoMethodError for `obj.no_such_event!`.
	ErrUndefinedEvent = errors.New("aasm: undefined event")

	// ErrInvalidTransition is returned (when whiny) when the event is defined but
	// no transition is available from the current state — either no transition
	// lists the state in its `from` set, or every candidate's guards blocked it.
	// It mirrors the gem's AASM::InvalidTransition.
	ErrInvalidTransition = errors.New("aasm: invalid transition")
)
