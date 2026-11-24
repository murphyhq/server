package murphy

import "google.golang.org/protobuf/types/known/timestamppb"

func (i *Incident) Open() bool {
	return i.GetStatus() != IncidentStatus_INCIDENT_STATUS_RESOLVED
}

func (i *Incident) Resolve(at *timestamppb.Timestamp) bool {
	if !i.Open() {
		return false
	}
	i.Status = IncidentStatus_INCIDENT_STATUS_RESOLVED
	i.ResolvedAt = at
	i.UpdatedAt = at
	return true
}

func (s IncidentStatus) Text() string {
	return enumText(s, "INCIDENT_STATUS_")
}

func IncidentStatusFromText(text string) IncidentStatus {
	return enumFromText[IncidentStatus](IncidentStatus_value, "INCIDENT_STATUS_", text)
}

func (t TimelineEntryType) Text() string {
	return enumText(t, "TIMELINE_ENTRY_TYPE_")
}

func TimelineEntryTypeFromText(text string) TimelineEntryType {
	return enumFromText[TimelineEntryType](TimelineEntryType_value, "TIMELINE_ENTRY_TYPE_", text)
}
