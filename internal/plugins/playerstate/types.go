package playerstate

type PlayerState struct {
	Bladder int64 `json:"bladder"`
	Alcohol int64 `json:"alcohol"`
	Weed    int64 `json:"weed"`
	Food    int64 `json:"food"`
	Lust    int64 `json:"lust"`
}

type playerStateRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
}

type playerStateMutationRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`

	Bladder *int64 `json:"bladder"`
	Alcohol *int64 `json:"alcohol"`
	Weed    *int64 `json:"weed"`
	Food    *int64 `json:"food"`
	Lust    *int64 `json:"lust"`
}

type playerStateResponse struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`

	Bladder int64 `json:"bladder"`
	Alcohol int64 `json:"alcohol"`
	Weed    int64 `json:"weed"`
	Food    int64 `json:"food"`
	Lust    int64 `json:"lust"`
}

func (r playerStateMutationRequest) hasAnyValue() bool {
	return r.Bladder != nil || r.Alcohol != nil || r.Weed != nil || r.Food != nil || r.Lust != nil
}
