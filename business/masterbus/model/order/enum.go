package order

type CorridorReferencePoint string

const (
	CorridorReferencePointKinematicCenter CorridorReferencePoint = "KINEMATICCENTER"
	CorridorReferencePointContour         CorridorReferencePoint = "CONTOUR"
)

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
