package httpapi

import "github.com/armature/armature/backend/internal/issue"

// toInput turns the patch into the service's terms; a field the caller did not
// send stays nil, which the service reads as "leave it alone".
func (req updateIssueRequest) toInput() issue.UpdateInput {
	in := issue.UpdateInput{
		Summary:           req.Summary,
		TypeID:            req.TypeID,
		ReporterID:        req.ReporterID,
		ExpectedUpdatedAt: req.ExpectedUpdatedAt,
	}
	if req.Description.Set {
		value := req.Description.Value
		in.Description = &value
	}
	if req.Priority != nil {
		priority := issue.Priority(*req.Priority)
		in.Priority = &priority
	}
	if req.Assignee.Set {
		value := req.Assignee.Value
		in.Assignee = &value
	}
	if req.DueDate.Set {
		value := req.DueDate.Value
		in.DueDate = &value
	}
	if req.TimeEstimateMinutes.Set {
		value := req.TimeEstimateMinutes.Value
		in.TimeEstimate = &value
	}
	if req.TimeRemainingMinutes.Set {
		value := req.TimeRemainingMinutes.Value
		in.TimeRemaining = &value
	}
	return in
}
