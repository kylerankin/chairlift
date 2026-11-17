package agentmode

// DispatchAction indicates what action should be taken for --ask-bluefin.
type DispatchAction int

const (
	// DispatchLaunch indicates that Goose Desktop should be launched with the active model.
	DispatchLaunch DispatchAction = iota
	// DispatchPresentAgents indicates that Control Center should open to the Agents page.
	DispatchPresentAgents
)

// String returns a human-readable representation of DispatchAction.
func (a DispatchAction) String() string {
	switch a {
	case DispatchLaunch:
		return "Launch"
	case DispatchPresentAgents:
		return "PresentAgents"
	default:
		return "Unknown"
	}
}

// DispatchDecision is the result of evaluating the --ask-bluefin intent.
type DispatchDecision struct {
	Action DispatchAction
	Model  string
	State  State
	Reason string
}

// Dispatch evaluates the readiness facts and decides whether to launch Goose
// or present Control Center on the Agents page.
func Dispatch(facts ReadinessFacts) DispatchDecision {
	state := Evaluate(facts)
	if state == StateReady {
		return DispatchDecision{
			Action: DispatchLaunch,
			Model:  facts.ActiveModel,
			State:  StateReady,
		}
	}
	return DispatchDecision{
		Action: DispatchPresentAgents,
		State:  state,
		Reason: state.MissingPrerequisite(),
	}
}
