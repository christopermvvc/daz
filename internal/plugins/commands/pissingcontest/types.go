package pissingcontest

import (
	"math/rand"
	"strings"
	"time"
)

const (
	pluginName             = "pissingcontest"
	activeChallengeStatus  = "pending"
	completedChallengeStat = "completed"
	challengeTimeout       = 30 * time.Second
	defaultCooldown        = 5 * time.Minute
	contestEventBaseDelay  = 4 * time.Second
	contestEventStepDelay  = 1 * time.Second
)

type Config struct {
	BotUsername              string `json:"bot_username"`
	ChallengeDurationSeconds int    `json:"challenge_duration_seconds"`
	CooldownMinutes          int    `json:"cooldown_minutes"`
}

type contestResult struct {
	distance float64
	volume   float64
	aim      float64
	duration float64

	ignoreDebuffs      bool
	ignoreWeather      bool
	copyBestStat       bool
	coldTurtle         bool
	wrongDirection     bool
	shaking            bool
	straightUp         bool
	wrongDirectionCeil float64
}

type contestModifiers struct {
	allFailureChance     float64
	forfeitChance        float64
	malfunctionChance    float64
	wrongDirectionChance float64
	copyBestStat         bool
	ignoreDebuffs        bool
	ignoreWeather        bool
	coldTurtle           bool
	randomFailures       bool
	multipleFailures     bool
	perfectOrFail        bool
	startStrongFail      bool
	mutualMalfunction    bool
	weatherChangeChance  float64
	badRNG               bool
	straightUp           bool
	shaking              bool
	wrongWay             bool
	quitIfLosing         bool
}

type contestEvent struct {
	Message string
	Forfeit bool
	Target  string
}

func (s contestResult) score() int {
	distanceScore := (s.distance / 5.0) * 1000.0
	volumeScore := (s.volume / 2000.0) * 1000.0
	aimScore := (s.aim / 100.0) * 1000.0
	durationScore := (s.duration / 30.0) * 1000.0

	total := (distanceScore * 0.4) + (volumeScore * 0.25) +
		(aimScore * 0.2) + (durationScore * 0.15)

	return int(total)
}

type challenge struct {
	challenger               string
	challenged               string
	amount                   int64
	room                     string
	createdAt                time.Time
	expiresAt                time.Time
	status                   string
	timer                    *time.Timer
	challengerCharacteristic characteristic
	challengedCharacteristic characteristic
	location                 location
	weather                  weatherRoll
	challengerCondition      condition
	challengedCondition      condition
}

type characterEffect struct {
	Name        string
	Description string
	Effects     map[string]float64
	Weight      int
}

type characteristic struct {
	Name            string
	Category        string
	Description     string
	Rarity          string
	ForceComment    bool
	Effects         map[string]float64
	Weight          int
	MutualCondition string
	SelfCondition   string
}

type condition struct {
	Name        string
	Type        string
	Description string
	Message     string
	Fine        int64
	FineMessage string
	FineChance  float64
	Mutual      bool
	Weight      int
	Effects     map[string]float64
}

type location struct {
	Name             string
	Description      string
	Effects          map[string]float64
	CloggedChance    float64
	WicketFailChance float64
	BiteChance       float64
	SprinklerChance  float64
	CopChance        float64
	RunOverChance    float64
	TinnieHitChance  float64
	AngryChance      float64
	Fine             int64
	FineMessage      string
	FineChance       float64
}

type weather struct {
	Name        string
	Description string
	Special     bool
	Message     string
	Effects     map[string]float64
	// Legacy compatibility fields for older event logic.
	MightHitOpponentChance float64
	TurtleModeChance       float64
	PassOutChance          float64
	DistanceTailwind       float64
	DistanceHeadwind       float64
	DistanceVariance       float64
	WindVarianceMultiplier bool
	InstantForfeitChance   float64
	MalfunctionChance      float64
	RandomEffectChance     float64
}

type weatherRoll struct {
	Wind        weather
	Temperature weather
	Special     *weather
}

type activeChallenge struct {
	Challenger        string
	Challenged        string
	Amount            int64
	Room              string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	Status            string
	ChallengerChar    characteristic
	ChallengedChar    characteristic
	ChallengerCond    condition
	ChallengedCond    condition
	Location          location
	Weather           weatherRoll
	Timer             *time.Timer
	ChallengerScore   int
	ChallengedScore   int
	ChallengerStats   contestResult
	ChallengedStats   contestResult
	ChallengerMods    contestModifiers
	ChallengedMods    contestModifiers
	ChallengerFailMsg string
	ChallengedFailMsg string
	locationEvents    []contestEvent
	weatherEvents     []contestEvent
}

func normalizeChannel(room string) string {
	return strings.ToLower(strings.TrimSpace(room))
}

func normalizeUsername(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "@")
	return strings.ToLower(value)
}

func randomFromSlice[T any](items []T) T {
	return items[rand.Intn(len(items))]
}

func randBetween(min, max float64) float64 {
	return min + rand.Float64()*(max-min)
}
