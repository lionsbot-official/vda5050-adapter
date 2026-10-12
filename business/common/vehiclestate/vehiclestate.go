package vehiclestate

import (
	"reflect"
	"sync"

	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
)

type Store struct {
	mu            sync.Mutex
	state         state.State
	changes       chan struct{}
	readiness     chan struct{}
	cancelled     bool // current order was cancelled; updates to it must reject
	cleared       bool
	desiredMode   state.OperatingMode
	pendingMode   state.OperatingMode
	clearLastNode bool
}

func New() *Store {
	return &Store{
		changes:     make(chan struct{}, 1),
		readiness:   make(chan struct{}, 1),
		desiredMode: state.OperatingModeAutomatic,
		state: state.State{
			OrderID:             "",
			OrderUpdateID:       0,
			LastNodeID:          "",
			LastNodeSequenceID:  0,
			NodeStates:          []state.NodeState{},
			EdgeStates:          []state.EdgeState{},
			Driving:             false,
			ActionStates:        []state.ActionState{},
			InstantActionStates: []state.ActionState{},
			PowerSupply:         state.PowerSupply{StateOfCharge: 100, Charging: false},
			OperatingMode:       state.OperatingModeAutomatic,
			Errors:              []state.Error{},
			SafetyState:         state.SafetyState{ActiveEmergencyStop: state.ActiveEmergencyStopNone, FieldViolation: false},
		},
	}
}

func (s *Store) Snapshot() state.State {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.state
	snap.Errors = copySlice(s.state.Errors)
	snap.NodeStates = copySlice(s.state.NodeStates)
	snap.EdgeStates = copySlice(s.state.EdgeStates)
	snap.ActionStates = copySlice(s.state.ActionStates)
	snap.InstantActionStates = copySlice(s.state.InstantActionStates)

	return snap
}

func (s *Store) AddError(e state.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.notify()
	for i := range s.state.Errors {
		if s.state.Errors[i].ErrorType == e.ErrorType &&
			reflect.DeepEqual(s.state.Errors[i].ErrorReferences, e.ErrorReferences) {
			wasBlocking := s.state.Errors[i].ErrorLevel == state.ErrorLevelFatal || s.state.Errors[i].ErrorLevel == state.ErrorLevelCritical
			isBlocking := e.ErrorLevel == state.ErrorLevelFatal || e.ErrorLevel == state.ErrorLevelCritical
			s.state.Errors[i] = e
			if wasBlocking != isBlocking {
				s.notifyReadiness()
			}
			return
		}
	}
	s.state.Errors = append(s.state.Errors, e)
	if e.ErrorLevel == state.ErrorLevelFatal || e.ErrorLevel == state.ErrorLevelCritical {
		s.notifyReadiness()
	}
}

func (s *Store) AddInstantActionState(as state.ActionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.InstantActionStates = append(s.state.InstantActionStates, as)
	s.notify()
}

// BeginOrderCancellation rejects further updates while running actions stop.
func (s *Store) BeginOrderCancellation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
	for i := range s.state.ActionStates {
		if s.state.ActionStates[i].ActionStatus == state.ActionStatusWaiting {
			s.state.ActionStates[i].ActionStatus = state.ActionStatusFailed
		}
	}
	s.notify()
}

// CancelOrder empties the order's node/edge states and fails its non-terminal
// actions; orderId, orderUpdateId and lastNode are kept (VDA order-cancellation
// semantics). The vehicle is idle afterwards.
func (s *Store) CancelOrder() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.NodeStates = []state.NodeState{}
	s.state.EdgeStates = []state.EdgeState{}
	for i := range s.state.ActionStates {
		st := s.state.ActionStates[i].ActionStatus
		if st != state.ActionStatusFinished && st != state.ActionStatusFailed {
			s.state.ActionStates[i].ActionStatus = state.ActionStatusFailed
		}
	}
	s.cancelled = true
	s.notify()
}

// SetPaused records the vehicle's pause mode; explicit true/false once
// touched (a linked state the fleet master relies on).
func (s *Store) SetPaused(paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Paused = &paused
	s.notify()
}

// OrderCancelled reports whether the currently held order was cancelled;
// cleared when the next order is committed.
func (s *Store) OrderCancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// IsIdle reports the VDA idle condition: no node/edge states and every order
// action terminal. Only an idle vehicle may accept a new order.
func IsIdle(st state.State) bool {
	if len(st.NodeStates) > 0 || len(st.EdgeStates) > 0 {
		return false
	}
	for _, a := range st.ActionStates {
		if a.ActionStatus != state.ActionStatusFinished && a.ActionStatus != state.ActionStatusFailed {
			return false
		}
	}
	return true
}

// SetActionStatus transitions an order action's status by actionId; unknown
// ids are a no-op. result lands in actionResult when non-empty.
func (s *Store) SetActionStatus(actionID string, status state.ActionStatus, result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.ActionStates {
		if s.state.ActionStates[i].ActionID != actionID {
			continue
		}
		s.state.ActionStates[i].ActionStatus = status
		if result != "" {
			s.state.ActionStates[i].ActionResult = result
		}
		s.notify()
		return
	}
}

// SetInstantActionStatus transitions an instant action's status by actionId;
// unknown ids are a no-op. result lands in actionResult when non-empty.
func (s *Store) SetInstantActionStatus(actionID string, status state.ActionStatus, result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.InstantActionStates {
		if s.state.InstantActionStates[i].ActionID != actionID {
			continue
		}
		s.state.InstantActionStates[i].ActionStatus = status
		if result != "" {
			s.state.InstantActionStates[i].ActionResult = result
		}
		s.notify()
		return
	}
}

func copySlice[T any](src []T) []T {
	out := make([]T, len(src))
	copy(out, src)
	return out
}

type OrderProjection struct {
	OrderID            string
	OrderUpdateID      int
	LastNodeID         string
	LastNodeSequenceID int
	NodeStates         []state.NodeState
	EdgeStates         []state.EdgeState
	ActionStates       []state.ActionState

	// KeepLastNode marks an order-update rebuild: the traversal-advanced
	// lastNodeId survives, and already-traversed node/edge states are not
	// resurrected from the merged order.
	KeepLastNode bool
}

// caller transfers ownership of p; do not retain or mutate p or its slices after this call.
func (s *Store) CommitOrderProjection(p OrderProjection) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cancelled = false
	if !p.KeepLastNode {
		s.cleared = false
	}
	s.state.OrderID = p.OrderID
	s.state.OrderUpdateID = p.OrderUpdateID
	if p.KeepLastNode {
		last := s.state.LastNodeSequenceID
		nodes := p.NodeStates[:0:0]
		for _, n := range p.NodeStates {
			if n.SequenceID > last {
				nodes = append(nodes, n)
			}
		}
		if nodes == nil {
			nodes = []state.NodeState{}
		}
		p.NodeStates = nodes
		edges := p.EdgeStates[:0:0]
		for _, e := range p.EdgeStates {
			if e.SequenceID > last {
				edges = append(edges, e)
			}
		}
		if edges == nil {
			edges = []state.EdgeState{}
		}
		p.EdgeStates = edges
		for i := range p.ActionStates {
			for _, previous := range s.state.ActionStates {
				if p.ActionStates[i].ActionID == previous.ActionID {
					p.ActionStates[i] = previous
					break
				}
			}
		}
	} else {
		s.state.LastNodeID = p.LastNodeID
		s.state.LastNodeSequenceID = p.LastNodeSequenceID
	}
	s.state.NodeStates = p.NodeStates
	s.state.EdgeStates = p.EdgeStates
	s.state.ActionStates = p.ActionStates

	// stale errors about rejected order messages get removed (accepting a new order is what makes them stale),
	// while errors about anything else like robot condition, mode, zones
	// are kept because accepting an order doesn't fix those things.
	kept := s.state.Errors[:0:0]
	for _, e := range s.state.Errors {
		if _, orderScoped := orderScopedErrorTypes[e.ErrorType]; !orderScoped {
			kept = append(kept, e)
		}
	}
	s.state.Errors = kept
}

// Changes signals that the state changed in a way that warrants an immediate
// publish. Signals coalesce: at most one is pending.
func (s *Store) Changes() <-chan struct{} {
	return s.changes
}

// ReadinessChanges wakes the executor without consuming publisher notifications.
func (s *Store) ReadinessChanges() <-chan struct{} { return s.readiness }

func (s *Store) notifyReadiness() {
	select {
	case s.readiness <- struct{}{}:
	default:
	}
}

func (s *Store) notify() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

// CommitTraversal records that the robot reached a node: lastNodeId advances
// and every node/edge state up to and including that sequenceId is removed.
func (s *Store) CommitTraversal(nodeID string, sequenceID int) {
	s.mu.Lock()
	s.state.LastNodeID = nodeID
	s.state.LastNodeSequenceID = sequenceID

	nodes := s.state.NodeStates[:0:0]
	for _, n := range s.state.NodeStates {
		if n.SequenceID > sequenceID {
			nodes = append(nodes, n)
		}
	}
	if nodes == nil {
		nodes = []state.NodeState{}
	}
	s.state.NodeStates = nodes

	edges := s.state.EdgeStates[:0:0]
	for _, e := range s.state.EdgeStates {
		if e.SequenceID > sequenceID {
			edges = append(edges, e)
		}
	}
	if edges == nil {
		edges = []state.EdgeState{}
	}
	s.state.EdgeStates = edges
	s.mu.Unlock()

	s.notify()
}

type Telemetry struct {
	PowerSupply           *state.PowerSupply
	MobileRobotPosition   *state.MobileRobotPosition
	SafetyState           *state.SafetyState
	OperatingMode         *state.OperatingMode
	Driving               *bool
	Velocity              *state.Velocity
	Loads                 []state.Load
	DistanceSinceLastNode *float64
	Information           []state.Info
	PlannedPath           *state.PlannedPath
}

// caller transfers ownership of t; do not retain or mutate t or its referents after this call.
func (s *Store) SetTelemetry(t Telemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// a delta in a fleet-relevant field publishes immediately; the rest ride
	// the heartbeat.
	relevant := (t.Driving != nil && *t.Driving != s.state.Driving) ||
		(t.OperatingMode != nil && *t.OperatingMode != s.state.OperatingMode) ||
		(t.SafetyState != nil && *t.SafetyState != s.state.SafetyState)
	if relevant {
		defer s.notify()
	}

	previousPose, previousMode, previousSafety := s.state.MobileRobotPosition, s.state.OperatingMode, s.state.SafetyState
	defer func() {
		pose := s.state.MobileRobotPosition
		if previousMode != s.state.OperatingMode || previousSafety != s.state.SafetyState || (previousPose == nil) != (pose == nil) ||
			(previousPose != nil && pose != nil && (previousPose.Localized != pose.Localized || previousPose.MapID != pose.MapID)) {
			s.notifyReadiness()
		}
	}()

	if t.PowerSupply != nil {
		s.state.PowerSupply = *t.PowerSupply
	}
	s.state.MobileRobotPosition = t.MobileRobotPosition
	s.updateLocalizationError()
	if t.SafetyState != nil {
		s.state.SafetyState = *t.SafetyState
	}
	if t.OperatingMode != nil {
		wasIdle := IsIdle(s.state) && !s.state.Driving && s.actionsTerminal()
		hadPending := s.pendingMode != ""
		s.desiredMode = *t.OperatingMode
		clearsOrder := *t.OperatingMode == state.OperatingModeManual || *t.OperatingMode == state.OperatingModeService ||
			*t.OperatingMode == state.OperatingModeTeachIn || *t.OperatingMode == state.OperatingModeStartup
		if clearsOrder && *t.OperatingMode != s.state.OperatingMode {
			s.pendingMode = *t.OperatingMode
			s.clearLastNode = s.clearLastNode || *t.OperatingMode == state.OperatingModeService || *t.OperatingMode == state.OperatingModeTeachIn || *t.OperatingMode == state.OperatingModeStartup
			s.cleared = true
			for i := range s.state.ActionStates {
				if s.state.ActionStates[i].ActionStatus == state.ActionStatusWaiting {
					s.state.ActionStates[i].ActionStatus = state.ActionStatusFailed
				}
			}
			for i := range s.state.InstantActionStates {
				if s.state.InstantActionStates[i].ActionStatus == state.ActionStatusWaiting {
					s.state.InstantActionStates[i].ActionStatus = state.ActionStatusFailed
				}
			}
			s.notifyReadiness()
		}
		if s.pendingMode == "" {
			s.state.OperatingMode = *t.OperatingMode
			if *t.OperatingMode == state.OperatingModeIntervened {
				s.state.ZoneRequests = nil
			}
		} else if !hadPending && wasIdle {
			s.completeModeTransition()
		}
	}
	s.clearAvailabilityError()
	if t.Driving != nil {
		s.state.Driving = *t.Driving
	}
	s.state.Velocity = t.Velocity
	s.state.Loads = t.Loads
	s.state.DistanceSinceLastNode = t.DistanceSinceLastNode
	s.state.Information = t.Information
	s.state.PlannedPath = t.PlannedPath
}

// Caller holds the state lock. Pending clearing modes still reject new orders.
func (s *Store) clearAvailabilityError() {
	if s.pendingMode != "" || (s.state.OperatingMode != state.OperatingModeAutomatic && s.state.OperatingMode != state.OperatingModeSemiAutomatic && s.state.OperatingMode != state.OperatingModeIntervened) {
		return
	}
	for i := len(s.state.Errors) - 1; i >= 0; i-- {
		if s.state.Errors[i].ErrorType == state.ErrorTypeMobileRobotNotAvailable {
			s.state.Errors = append(s.state.Errors[:i:i], s.state.Errors[i+1:]...)
			s.notify()
		}
	}
}

var orderScopedErrorTypes = map[state.ErrorType]struct{}{
	state.ErrorTypeValidationFailure:          {},
	state.ErrorTypeUnsupportedParameter:       {},
	state.ErrorTypeInvalidOrderAction:         {},
	state.ErrorTypeOutdatedOrderUpdate:        {},
	state.ErrorTypeSameOrderUpdateID:          {},
	state.ErrorTypeOrderUpdateFollowingCancel: {},
	state.ErrorTypeOtherOrderActive:           {},
	state.ErrorTypeStartNodeOutOfRange:        {},
	state.ErrorTypeNoRouteToTarget:            {},
	state.ErrorTypeUnknownMapID:               {},
	state.ErrorTypeNoOrderToCancel:            {},
	state.ErrorTypeInsufficientMemory:         {},
	state.ErrorTypeNodeUnreachable:            {},
}

// RequestedOperatingMode includes a latched clear even if telemetry already returned to AUTO.
func (s *Store) RequestedOperatingMode() state.OperatingMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingMode != "" {
		return s.pendingMode
	}
	return s.desiredMode
}

func (s *Store) ModeChangePending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingMode != ""
}

func (s *Store) OrderCleared() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleared
}

// CompleteModeTransition is called only after the executor has no operation awaiting completion.
func (s *Store) CompleteModeTransition() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingMode != "" && s.actionsTerminal() {
		s.completeModeTransition()
	}
}

func (s *Store) actionsTerminal() bool {
	for _, actions := range [][]state.ActionState{s.state.ActionStates, s.state.InstantActionStates} {
		for _, action := range actions {
			if action.ActionStatus != state.ActionStatusFinished && action.ActionStatus != state.ActionStatusFailed {
				return false
			}
		}
	}
	return true
}

func (s *Store) completeModeTransition() {
	if s.clearLastNode {
		s.state.LastNodeID = ""
	}
	s.state.NodeStates = []state.NodeState{}
	s.state.EdgeStates = []state.EdgeState{}
	s.state.ZoneRequests = nil
	s.state.EdgeRequests = nil
	s.state.OperatingMode = s.desiredMode
	s.pendingMode = ""
	s.clearLastNode = false
	s.clearAvailabilityError()
	s.notifyReadiness()
	s.notify()
}

// Caller holds the state lock; unknown startup pose is not a reported failure.
func (s *Store) updateLocalizationError() {
	pose := s.state.MobileRobotPosition
	if pose == nil {
		return
	}
	for i, err := range s.state.Errors {
		if err.ErrorType != state.ErrorTypeLocalizationError {
			continue
		}
		if pose.Localized {
			s.state.Errors = append(s.state.Errors[:i:i], s.state.Errors[i+1:]...)
			s.notify()
		}
		return
	}
	if !pose.Localized {
		s.state.Errors = append(s.state.Errors, state.Error{
			ErrorType:        state.ErrorTypeLocalizationError,
			ErrorLevel:       state.ErrorLevelFatal,
			ErrorDescription: "robot is not localized",
		})
		s.notify()
	}
}

// CompleteRelocalization publishes the confirmed pose and action outcome together.
func (s *Store) CompleteRelocalization(actionID string, pose state.MobileRobotPosition, lastNodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.InstantActionStates {
		if s.state.InstantActionStates[i].ActionID == actionID {
			s.state.MobileRobotPosition = &pose
			s.updateLocalizationError()
			s.state.LastNodeID = lastNodeID
			s.state.LastNodeSequenceID = 0
			s.state.InstantActionStates[i].ActionStatus = state.ActionStatusFinished
			s.state.InstantActionStates[i].ActionResult = ""
			s.notifyReadiness()
			s.notify()
			return
		}
	}
}
