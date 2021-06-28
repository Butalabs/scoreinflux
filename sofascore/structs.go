package sofascore

type Lineups struct {
	Players []*LineupPlayer
}
type LineupPlayer struct {
	Player *Player
	Substitute bool
}
type Player struct {
	Name string
	ID int
}

type Managers struct {
	AwayManager *Manager
	HomeManager *Manager
}
type Manager struct {
	ID int
	Name string
}

type Incidents []&Incident
type Incident struct {
	IncidentType string
	Length int
	IsHome bool
	PlayerIn *Player
	PlayerOut *Player
	Time int
}
