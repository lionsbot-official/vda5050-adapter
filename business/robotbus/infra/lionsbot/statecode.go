package lionsbot

// operation_state code columns
// v0 = LeoBot/LeoRay, v1 = R3/Nilfisk/Cub/Rex/N3/SC25/VP25, v2 = R5/S3/R2
type codeVersion int

const (
	codeV0 codeVersion = iota
	codeV1
	codeV2
)

type canonicalState string

const (
	stateResting            canonicalState = "Resting"
	stateUpdating           canonicalState = "Updating"
	stateMoving             canonicalState = "Moving"
	stateMovingPaused       canonicalState = "Moving Paused"
	stateCleaning           canonicalState = "Cleaning"
	stateCleaningPaused     canonicalState = "Cleaning Paused"
	stateTeleop             canonicalState = "Teleoping"
	stateTeleopPaused       canonicalState = "Teleop Paused"
	stateTeleopClean        canonicalState = "Teleop Clean"
	stateTeleopCleanPaused  canonicalState = "Teleop Clean Paused"
	statePulling            canonicalState = "Pulling"
	statePullingPaused      canonicalState = "Pulling Paused"
	stateGoingHome          canonicalState = "Going Home"
	stateAtHome             canonicalState = "At Home"
	stateGreeting           canonicalState = "Greeting"
	stateMapping            canonicalState = "Mapping"
	stateExplore            canonicalState = "Explore"
	stateExplorePaused      canonicalState = "Explore Paused"
	stateDocking            canonicalState = "Docking"
	stateDockingMove        canonicalState = "Docking Move"
	stateDockingInProgress  canonicalState = "Docking In Progress"
	stateDockingPaused      canonicalState = "Docking Paused"
	stateDocked             canonicalState = "Docked"
	stateUndocking          canonicalState = "Undocking"
	stateManualDock         canonicalState = "Manual Dock"
	stateDemo               canonicalState = "Demo"
	stateMaintenance        canonicalState = "Maintenance"
	stateCalibration        canonicalState = "Calibration"
	stateGetLocationImage   canonicalState = "Get Location Image"
	stateGantryTraverse     canonicalState = "Gantry Traverse"
	stateWaitingForLift     canonicalState = "Waiting For Lift"
	stateEnteringLift       canonicalState = "Entering Lift"
	stateInsideLift         canonicalState = "Inside Lift"
	stateExitingLift        canonicalState = "Exiting Lift"
	stateExitedLift         canonicalState = "Exited Lift"
	stateExpandMap          canonicalState = "Expand Map"
	stateCharging           canonicalState = "Charging"
	stateDisinfecting       canonicalState = "Disinfecting"
	stateDisinfectingPaused canonicalState = "Disinfecting Paused"
	stateSleeping           canonicalState = "Sleeping"
	stateConnecting         canonicalState = "Connecting"
	stateCritical           canonicalState = "Critical"
	stateEstop              canonicalState = "Estop"
	stateError              canonicalState = "Error"
	stateUndefined          canonicalState = "Undefined"
)

var v0Codes = map[int]canonicalState{
	0:  stateResting,
	1:  stateMoving,
	2:  stateMovingPaused,
	3:  stateCleaning,
	4:  stateCleaningPaused,
	5:  stateTeleop,
	6:  stateTeleopPaused,
	7:  statePulling,
	8:  statePullingPaused,
	9:  stateGoingHome,
	10: stateAtHome,
	11: stateGreeting,
	12: stateDocking,
	13: stateUndocking,
	15: stateGantryTraverse,
	16: stateWaitingForLift,
	17: stateEnteringLift,
	18: stateInsideLift,
	19: stateExitingLift,
	20: stateExitedLift,
	25: stateCharging,
	31: stateDisinfecting,
	32: stateDisinfectingPaused,
	40: stateMapping,
	41: stateMaintenance,
	50: stateSleeping,
	98: stateConnecting,
	99: stateCritical,
}

var v1Codes = map[int]canonicalState{
	0:  stateResting,
	1:  stateUpdating,
	2:  stateMoving,
	3:  stateMovingPaused,
	4:  stateCleaning,
	5:  stateCleaningPaused,
	6:  stateTeleop,
	7:  stateTeleopClean,
	8:  stateTeleopCleanPaused,
	9:  stateMapping,
	10: stateDockingMove,
	11: stateDockingInProgress,
	12: stateDockingPaused,
	13: stateDocked,
	14: stateUndocking,
	15: stateDemo,
	16: stateMaintenance,
	17: stateCalibration,
	18: stateGetLocationImage,
	19: stateEnteringLift,
	20: stateExitingLift,
	21: stateGantryTraverse,
	22: stateWaitingForLift,
	23: stateInsideLift,
	24: stateExitedLift,
	97: stateCharging,
	98: stateEstop,
	99: stateError,
}

var v2Codes = map[int]canonicalState{
	0:   stateResting,
	1:   stateUpdating,
	2:   stateMoving,
	3:   stateMovingPaused,
	4:   stateCleaning,
	5:   stateCleaningPaused,
	6:   stateTeleop,
	7:   stateTeleopClean,
	8:   stateTeleopCleanPaused,
	9:   stateMapping,
	10:  stateMaintenance,
	11:  stateExplore,
	12:  stateExplorePaused,
	13:  stateDocking,
	14:  stateDockingPaused,
	15:  stateDocked,
	16:  stateUndocking,
	17:  stateDockingMove,
	18:  stateManualDock,
	19:  stateExpandMap,
	97:  stateCharging,
	98:  stateEstop,
	99:  stateError,
	100: stateUndefined,
}

func decodeState(v codeVersion, code int) (canonicalState, bool) {
	var m map[int]canonicalState
	switch v {
	case codeV0:
		m = v0Codes
	case codeV1:
		m = v1Codes
	case codeV2:
		m = v2Codes
	default:
		return "", false
	}
	s, ok := m[code]
	return s, ok
}
