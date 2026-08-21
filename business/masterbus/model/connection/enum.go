package connection

type ConnectionState string

const (
	ConnectionStateOnline           ConnectionState = "ONLINE"
	ConnectionStateOffline          ConnectionState = "OFFLINE"
	ConnectionStateHibernating      ConnectionState = "HIBERNATING"
	ConnectionStateConnectionBroken ConnectionState = "CONNECTION_BROKEN"
)
