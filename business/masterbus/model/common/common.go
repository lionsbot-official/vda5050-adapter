package common

type Header struct {
	HeaderID     uint32 `json:"headerId"`
	Timestamp    string `json:"timestamp"` //ISO8601 format (YYYY-MM-DDTHH:mm:ss.ffZ)
	Version      string `json:"version"`
	Manufacturer string `json:"manufacturer"`
	SerialNumber string `json:"serialNumber"`
}

type Trajectory struct {
	ControlPoints []ControlPoint `json:"controlPoints"`

	Degree     *int      `json:"degree,omitempty"`     //min 1
	KnotVector []float64 `json:"knotVector,omitempty"` //min 0.0, max 1.0
}

type ControlPoint struct {
	X      float64  `json:"x"`
	Y      float64  `json:"y"`
	Weight *float64 `json:"weight,omitempty"`
}
