package order

type ReleaseLossBehavior string

const (
	ReleaseLossBehaviorStop   ReleaseLossBehavior = "STOP"
	ReleaseLossBehaviorReturn ReleaseLossBehavior = "RETURN"
)

type CorridorReferencePoint string

const (
	CorridorReferencePointKinematicCenter CorridorReferencePoint = "KINEMATIC_CENTER"
	CorridorReferencePointContour         CorridorReferencePoint = "CONTOUR"
)

type OrientationType string

const (
	OrientationTypeGlobal     OrientationType = "GLOBAL"
	OrientationTypeTangential OrientationType = "TANGENTIAL"
)

type BlockingType string

const (
	BlockingTypeNone   BlockingType = "NONE"
	BlockingTypeSoft   BlockingType = "SOFT"
	BlockingTypeSingle BlockingType = "SINGLE"
	BlockingTypeHard   BlockingType = "HARD"
)
