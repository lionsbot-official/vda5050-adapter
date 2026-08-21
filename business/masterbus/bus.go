package masterbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	topichelper "github.com/lionsbot-official/vda5050-adapter/business/common/topic"
	"github.com/lionsbot-official/vda5050-adapter/business/common/vehiclestate"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/infra/transport"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/cleaningcatalog"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/common"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/connection"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/instantactions"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/order"
	"github.com/lionsbot-official/vda5050-adapter/business/masterbus/model/state"
	"github.com/lionsbot-official/vda5050-adapter/foundation/env"
	vdatime "github.com/lionsbot-official/vda5050-adapter/foundation/time"
)

const (
	qosOrder          byte = 0
	qosState          byte = 0
	qosInstantActions byte = 0
	qosConnection     byte = 1
	qosFactsheet      byte = 0 // VDA5050_EN.md:189
)

type Transport interface {
	Publish(topic string, payload any, qos byte, retained bool) error
	Subscribe(topic string, qos byte) (chan []byte, error)
	Connect() error
	Disconnect()
	SetOnConnect(f func())
}

type Bus struct {
	transport           Transport
	version             string
	serialNumber        string
	manufacturer        string
	connTopic           string
	stateTopic          string
	orderTopic          string
	instantActionsTopic string
	catalogTopic        string
	factsheetTopic      string

	robot   *robotState
	headers *headerCounter

	onOrderAccepted func(order.Order)
	onInstantAction func(order.Action)

	catalogMu   sync.Mutex
	lastCatalog *cleaningcatalog.Catalog // republished on (re)connect
}

// SetOrderAcceptedHook registers a callback invoked with the accepted (merged)
// order; wired to the executor from main.
func (b *Bus) SetOrderAcceptedHook(fn func(order.Order)) {
	b.onOrderAccepted = fn
}

func (b *Bus) notifyOrderAccepted() {
	if b.onOrderAccepted != nil {
		b.onOrderAccepted(b.robot.merged)
	}
}

// SetInstantActionHook registers a callback invoked with each accepted
// instant action; wired to the executor from main.
func (b *Bus) SetInstantActionHook(fn func(order.Action)) {
	b.onInstantAction = fn
}

func (b *Bus) notifyInstantAction(a order.Action) {
	if b.onInstantAction != nil {
		b.onInstantAction(a)
	}
}

// StartChangePublisher publishes state immediately whenever the store signals
// a relevant change (e.g. a traversed node), keeping the heartbeat as a floor.
func (b *Bus) StartChangePublisher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.robot.store.Changes():
			b.publishState()
		}
	}
}

func NewWithTransport(transport Transport, store *vehiclestate.Store, serialNumber, version, manufacturer string) (*Bus, error) {

	connTopic, err := topichelper.ToTopic(topichelper.TopicConnection, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("transport: error getting connection topic: %w", err)
	}
	stateTopic, err := topichelper.ToTopic(topichelper.TopicState, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("transport: error getting state topic: %w", err)
	}
	orderTopic, err := topichelper.ToTopic(topichelper.TopicOrder, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("transport: error getting order topic: %w", err)
	}
	instantActionsTopic, err := topichelper.ToTopic(topichelper.TopicInstantActions, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("transport: error getting instantActions topic: %w", err)
	}
	catalogTopic, err := topichelper.ToTopic(topichelper.TopicCleaningCatalog, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("transport: error getting cleaning catalog topic: %w", err)
	}
	factsheetTopic, err := topichelper.ToTopic(topichelper.TopicFactsheet, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("transport: error getting factsheet topic: %w", err)
	}
	return &Bus{
		transport:           transport,
		serialNumber:        serialNumber,
		version:             version,
		manufacturer:        manufacturer,
		connTopic:           connTopic,
		stateTopic:          stateTopic,
		orderTopic:          orderTopic,
		instantActionsTopic: instantActionsTopic,
		catalogTopic:        catalogTopic,
		factsheetTopic:      factsheetTopic,

		headers: newHeaderCounter(),
		robot:   newRobotState(store),
	}, nil
}

func New(configs env.Configs, store *vehiclestate.Store, serialNumber, manufacturer string) (*Bus, error) {
	version := configs.MasterConfig.Version

	willTopic, err := topichelper.ToTopic(topichelper.TopicConnection, version, manufacturer, serialNumber)
	if err != nil {
		return nil, fmt.Errorf("bus: error building will topic: %w", err)
	}

	willMsg := buildConnectionMsg(1, version, manufacturer, serialNumber, connection.ConnectionStateConnectionBroken)
	willPayload, err := json.Marshal(willMsg)
	if err != nil {
		return nil, fmt.Errorf("bus: error marshalling will payload: %w", err)
	}

	t, err := transport.New(configs, serialNumber, willTopic, willPayload)
	if err != nil {
		return nil, fmt.Errorf("bus: error building mqtt client: %w", err)
	}

	return NewWithTransport(t, store, serialNumber, version, manufacturer)
}

func (b *Bus) Start(ctx context.Context) error {
	b.transport.SetOnConnect(b.publishOnline)
	if err := b.transport.Connect(); err != nil {
		return fmt.Errorf("bus: failed to connect to transport: %w", err)
	}
	defer b.transport.Disconnect()

	// subscribe to order
	orderCh, err := b.transport.Subscribe(b.orderTopic, qosOrder)
	if err != nil {
		return fmt.Errorf("bus: failed to subscribe to order topic: %w", err)
	}

	instantCh, err := b.transport.Subscribe(b.instantActionsTopic, qosInstantActions)
	if err != nil {
		return fmt.Errorf("bus: failed to subscribe to instantActions topic: %w", err)
	}

	go b.StartOrderConsumer(ctx, orderCh)
	go b.StartInstantActionsConsumer(ctx, instantCh)
	go b.StartStateHeartbeat(ctx)
	go b.StartChangePublisher(ctx)

	<-ctx.Done()
	slog.Info("bus: stopping")

	b.publishConnectionOffline() // explicit before the deferred disconnect
	return nil
}

func (b *Bus) StartOrderConsumer(ctx context.Context, orders <-chan []byte) {
	for {
		select {
		case msg, ok := <-orders:
			if !ok {
				return
			}
			b.handleOrder(msg)
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bus) StartInstantActionsConsumer(ctx context.Context, msgs <-chan []byte) {
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			b.handleInstantActions(msg)
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bus) handleInstantActions(msg []byte) {
	var ia instantactions.InstantActions
	if err := json.Unmarshal(msg, &ia); err != nil {
		slog.Error("bus: failed to unmarshal instant actions", "err", err)
		b.robot.addError(state.Error{
			ErrorType:        state.ErrorTypeValidationFailure,
			ErrorLevel:       state.ErrorLevelWarning,
			ErrorDescription: err.Error(),
		})
		b.publishState()
		return
	}

	for _, a := range ia.Actions {
		if !supportedActions[a.ActionType].instant {
			slog.Warn("instant action rejected: unsupported", "actionId", a.ActionID, "actionType", a.ActionType)
			b.robot.store.AddInstantActionState(state.ActionState{
				ActionID:     a.ActionID,
				ActionType:   a.ActionType,
				ActionStatus: state.ActionStatusFailed,
			})
			b.robot.addError(state.Error{
				ErrorType:        state.ErrorTypeInvalidInstantAction,
				ErrorLevel:       state.ErrorLevelWarning,
				ErrorDescription: fmt.Sprintf("bus: unsupported instant action %q", a.ActionType),
				ErrorReferences: []state.ErrorReference{
					{ReferenceKey: "actionId", ReferenceValue: a.ActionID},
				},
			})
			continue
		}

		// factsheetRequest is pure adapter-side data: served here (publish +
		// FINISHED, RUNNING skipped), never forwarded to the executor.
		if a.ActionType == "factsheetRequest" {
			slog.Info("factsheetRequest served", "actionId", a.ActionID)
			b.publishFactsheet()
			b.robot.store.AddInstantActionState(state.ActionState{
				ActionID:     a.ActionID,
				ActionType:   a.ActionType,
				ActionStatus: state.ActionStatusFinished,
			})
			continue
		}

		slog.Info("instant action accepted", "actionId", a.ActionID, "actionType", a.ActionType)
		b.robot.store.AddInstantActionState(state.ActionState{
			ActionID:     a.ActionID,
			ActionType:   a.ActionType,
			ActionStatus: state.ActionStatusWaiting,
		})
		b.notifyInstantAction(a)
	}

	b.publishState()
}

func (b *Bus) rejectOrder(o order.Order, errorType state.ErrorType, reason error) {
	b.robot.addError(state.Error{
		ErrorType:        errorType,
		ErrorLevel:       state.ErrorLevelWarning,
		ErrorDescription: reason.Error(),
		ErrorReferences: []state.ErrorReference{
			{ReferenceKey: "orderId", ReferenceValue: o.OrderID},
			{ReferenceKey: "orderUpdateId", ReferenceValue: fmt.Sprintf("%d", o.OrderUpdateID)},
		},
	})
	b.publishState()
}

func (b *Bus) handleOrder(msg []byte) {
	var o order.Order
	if err := json.Unmarshal(msg, &o); err != nil {
		slog.Error("bus: failed to unmarshal order", "err", err)
		b.robot.addError(state.Error{
			ErrorType:        state.ErrorTypeValidationFailure,
			ErrorLevel:       state.ErrorLevelWarning,
			ErrorDescription: err.Error(),
		})
		b.publishState()
		return
	}

	slog.Info("order received", "orderId", o.OrderID, "orderUpdateId", o.OrderUpdateID)

	if err := validateShape(o); err != nil {
		slog.Warn("order rejected", "orderId", o.OrderID, "orderUpdateId", o.OrderUpdateID)
		b.rejectOrder(o, state.ErrorTypeValidationFailure, err)
		return
	}

	if actionType, ok := unsupportedOrderAction(o); ok {
		slog.Warn("order rejected: unsupported action", "orderId", o.OrderID, "actionType", actionType)
		b.rejectOrder(o, state.ErrorTypeInvalidOrderAction, fmt.Errorf("bus: unsupported order action %q", actionType))
		return
	}

	decision, errorType := b.robot.decideOrder(o)
	switch decision {
	case decisionReject:
		slog.Warn("order rejected", "orderId", o.OrderID, "orderUpdateId", o.OrderUpdateID, "errorType", errorType)
		b.rejectOrder(o, errorType, fmt.Errorf("order rejected: %s", errorType))
	case decisionIgnore:
		slog.Info("duplicate order ignored", "orderId", o.OrderID)
	case decisionAcceptUpdate:
		b.robot.applyOrderUpdate(o)
		b.publishState()
		b.notifyOrderAccepted()
		slog.Info("order updated", "orderId", o.OrderID, "orderUpdateId", o.OrderUpdateID)
	case decisionAcceptNew:
		if err := validateNewOrder(o); err != nil {
			slog.Warn("order rejected", "orderId", o.OrderID, "err", err)
			b.rejectOrder(o, state.ErrorTypeValidationFailure, err)
			return
		}
		b.robot.applyOrder(o)
		b.publishState()
		b.notifyOrderAccepted()
		slog.Info("order accepted", "orderId", o.OrderID)
	}
}

func (b *Bus) StartStateHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.publishState()
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bus) publishState() {
	st := b.robot.snapshot()

	st.Header = common.Header{
		HeaderID:     b.headers.next(b.stateTopic),
		Timestamp:    vdatime.Now(),
		Version:      b.version,
		Manufacturer: b.manufacturer,
		SerialNumber: b.serialNumber,
	}

	if err := b.transport.Publish(b.stateTopic, st, qosState, false); err != nil {
		slog.Error("bus: failed to publish state", "err", err)
	}
}

func (b *Bus) publishOnline() {
	if err := b.transport.Publish(b.connTopic, b.buildConnection(connection.ConnectionStateOnline), qosConnection, true); err != nil {
		slog.Error("bus: failed to publish online", "err", err)
	}
	b.publishFactsheet()
	b.republishCatalog() // a reconnect drops retained state on some brokers; re-assert it
}

// cleaningCatalogMsg wraps the vendor catalog with a minimal envelope; the topic
// already carries the serial, but a self-describing payload is friendlier.
type cleaningCatalogMsg struct {
	SerialNumber string `json:"serialNumber"`
	Timestamp    string `json:"timestamp"`
	cleaningcatalog.Catalog
}

// PublishCleaningCatalog publishes the active map's cleaning metadata (retained)
// on the vendor cleaningcatalog topic, and buffers it for republish on connect.
// Wired as the driver's catalog-changed hook from main.
func (b *Bus) PublishCleaningCatalog(cat cleaningcatalog.Catalog) error {
	b.catalogMu.Lock()
	b.lastCatalog = &cat
	b.catalogMu.Unlock()
	return b.publishCatalog(cat)
}

func (b *Bus) publishCatalog(cat cleaningcatalog.Catalog) error {
	msg := cleaningCatalogMsg{SerialNumber: b.serialNumber, Timestamp: vdatime.Now(), Catalog: cat}
	if err := b.transport.Publish(b.catalogTopic, msg, qosState, true); err != nil {
		return fmt.Errorf("bus: failed to publish cleaning catalog: %w", err)
	}
	return nil
}

func (b *Bus) republishCatalog() {
	b.catalogMu.Lock()
	cat := b.lastCatalog
	b.catalogMu.Unlock()
	if cat == nil {
		return
	}
	if err := b.publishCatalog(*cat); err != nil {
		slog.Error("bus: failed to republish cleaning catalog", "err", err)
	}
}

func (b *Bus) publishConnectionOffline() {
	if err := b.transport.Publish(b.connTopic, b.buildConnection(connection.ConnectionStateOffline), qosConnection, true); err != nil {
		slog.Error("bus: failed to publish connection offline", "err", err)
	}
}

func (b *Bus) buildConnection(state connection.ConnectionState) connection.Connection {
	return buildConnectionMsg(b.headers.next(b.connTopic), b.version, b.manufacturer, b.serialNumber, state)
}

func buildConnectionMsg(headerID uint32, version, manufacturer, serialNumber string, state connection.ConnectionState) connection.Connection {
	return connection.Connection{
		Header: common.Header{
			HeaderID:     headerID,
			Timestamp:    vdatime.Now(),
			Version:      version,
			Manufacturer: manufacturer,
			SerialNumber: serialNumber,
		},
		ConnectionState: state,
	}

}

func validateShape(o order.Order) error {
	lenOfNodes := len(o.Nodes)
	if lenOfNodes < 1 {
		return fmt.Errorf("bus: validateOrder: expect len(Nodes) to >= 1, actual %d", lenOfNodes)
	}
	lenOfEdges := len(o.Edges)
	if lenOfEdges != lenOfNodes-1 {
		return fmt.Errorf("bus: validateOrder: expect len(Edges) to be %d, actual %d", lenOfNodes-1, lenOfEdges)
	}
	return nil
}

func validateNewOrder(o order.Order) error {
	var first *order.Node
	for i := range o.Nodes {
		// using index here to reference instead of copy
		if o.Nodes[i].SequenceID == 0 {
			first = &o.Nodes[i]
			break
		}
	}
	if first == nil {
		return fmt.Errorf("bus: validateOrder: order missing node with sequenceId 0")
	}
	if !first.Released {
		return fmt.Errorf("bus: validateOrder: first node (sequenceId 0) must be released")
	}

	return nil
}
