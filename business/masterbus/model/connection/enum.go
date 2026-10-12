package connection

type ConnectionState string

const (
	ConnectionStateOnline           ConnectionState = "ONLINE"
	ConnectionStateOffline          ConnectionState = "OFFLINE"
	ConnectionStateConnectionBroken ConnectionState = "CONNECTIONBROKEN"
)
