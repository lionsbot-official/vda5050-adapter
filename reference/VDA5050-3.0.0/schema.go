package vda5050

import _ "embed"

//go:embed schemas/order.schema
var OrderSchema string

//go:embed schemas/instantActions.schema
var InstantActionsSchema string
