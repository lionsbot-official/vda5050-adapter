package order

type OrientationType string

const (
	OrientationTypeGlobal     OrientationType = "GLOBAL"
	OrientationTypeTangential OrientationType = "TANGENTIAL"
)

type BlockingType string

const (
	BlockingTypeNone BlockingType = "NONE"
	BlockingTypeSoft BlockingType = "SOFT"
	BlockingTypeHard BlockingType = "HARD"
)
