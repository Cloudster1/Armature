package privacy

import "time"

// WorkingHours is the week an organization set for the person, and the
// holiday calendar it gave them; none named is the organization's default.
type WorkingHours struct {
	Organization string         `json:"organization"`
	Calendar     string         `json:"calendar,omitempty"`
	Minutes      map[string]int `json:"minutes"`
}

// Absence is days the person is away, and who wrote them down.
type Absence struct {
	Organization string `json:"organization"`
	StartsOn     string `json:"startsOn"`
	EndsOn       string `json:"endsOn"`
	HalfDay      bool   `json:"halfDay"`
	RecordedBy   string `json:"recordedBy"`
}

// Share is the part of the person's week a project was given.
type Share struct {
	Organization string `json:"organization"`
	Project      string `json:"project"`
	Percent      int    `json:"percent"`
}

// Company is a desk's company the person is in, whose members read the
// requests a colleague shared with it.
type Company struct {
	Desk    string    `json:"desk"`
	Company string    `json:"company"`
	AddedAt time.Time `json:"addedAt"`
}
