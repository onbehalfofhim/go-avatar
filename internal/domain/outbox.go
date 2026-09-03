package domain

type OutboxEvent struct {
	MessageID  string
	RoutingKey string
	Payload    []byte
}
