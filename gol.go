package gol

import (
	"github.com/ianremmler/chipmunk"
	"github.com/ianremmler/gordian"

	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

const (
	simTime       = time.Second / 60
	updateTime    = time.Second / 24
	pauseTime     = time.Second
	headStartTime = time.Second
	maxScore      = 10
	fieldWidth    = 1000
	fieldHeight   = 500
	edgeRadius    = 8
	goalSize      = 200
	playerRadius  = 10
	playerMass    = 1
	ballRadius    = 10
	ballMass      = 0.1
)

const (
	normLayer = 1 << iota
	goalLayer
)

type player struct {
	id          gordian.ClientId
	team        int
	body        chipmunk.Body
	shape       chipmunk.Shape
	cursorBody  chipmunk.Body
	cursorJoint chipmunk.Constraint
}

func (p *player) place() {
	widthRange := 0.5*fieldWidth - 2*playerRadius
	heightRange := 0.5*fieldHeight - playerRadius
	sign := float64(2*p.team - 1)
	pos := chipmunk.Vect{
		(widthRange*rand.Float64() + playerRadius) * sign,
		heightRange * (2*rand.Float64() - 1),
	}
	minDist := 0.25*fieldHeight + playerRadius
	dist := pos.Length()
	if dist < minDist {
		pos = pos.Mul(minDist / dist)
	}
	p.body.SetPosition(pos)
}

func (p *player) enableCursorJoint(enable bool) {
	sp := p.body.Space()
	isEnabled := sp.Contains(p.cursorJoint.(chipmunk.PivotJoint))
	switch {
	case enable && !isEnabled:
		sp.AddConstraint(p.cursorJoint)
	case !enable && isEnabled:
		sp.RemoveConstraint(p.cursorJoint)
	}
}

type ball struct {
	body  chipmunk.Body
	shape chipmunk.Shape
}

type Player struct {
	Pos  chipmunk.Vect
	Team int
}

type Ball struct {
	Pos chipmunk.Vect
}

type configMsg struct {
	Id           string
	FieldWidth   float64
	FieldHeight  float64
	GoalSize     float64
	PlayerRadius float64
	BallRadius   float64
}

type stateMsg struct {
	Players map[string]Player
	Ball    Ball
	Score   []int
}

type Gol struct {
	players     map[gordian.ClientId]*player
	ball        ball
	score       []int
	pauseTicks  []int
	simTimer    <-chan time.Time
	updateTimer <-chan time.Time
	curId       int
	space       *chipmunk.Space
	sync.Mutex
	*gordian.Gordian
}

func New() *Gol {
	g := &Gol{
		players:     map[gordian.ClientId]*player{},
		score:       []int{0, 0},
		pauseTicks:  []int{0, 0},
		simTimer:    time.Tick(simTime),
		updateTimer: time.Tick(updateTime),
		Gordian:     gordian.New(24),
	}
	g.setup()
	return g
}

func (g *Gol) setup() {
	g.space = chipmunk.SpaceNew()
	g.space.SetDamping(0.1)
	hfw, hfh, hgs := 0.5*fieldWidth, 0.5*fieldHeight, 0.5*goalSize
	sidePts := []chipmunk.Vect{{-hfw, hgs}, {-hfw, hfh}, {hfw, hfh}, {hfw, hgs}}
	numSideSegs := len(sidePts) - 1
	for i := 0; i < 2; i++ {
		sign := float64(2*i - 1)
		for j := 0; j < numSideSegs; j++ {
			p0, p1 := sidePts[j], sidePts[j+1]
			p0.Y *= sign
			p1.Y *= sign
			fieldSeg := chipmunk.SegmentShapeNew(g.space.StaticBody(), p0, p1, edgeRadius)
			fieldSeg.SetLayers(normLayer)
			fieldSeg.SetElasticity(1.0)
			fieldSeg.SetFriction(1.0)
			g.space.AddShape(fieldSeg)
		}
		p0, p1 := chipmunk.Vect{sign * hfw, -hgs}, chipmunk.Vect{sign * hfw, hgs}
		goal := chipmunk.SegmentShapeNew(g.space.StaticBody(), p0, p1, edgeRadius)
		goal.SetLayers(goalLayer)
		goal.SetElasticity(1.0)
		goal.SetFriction(1.0)
		g.space.AddShape(goal)
	}
	moment := chipmunk.MomentForCircle(ballMass, 0, ballRadius, chipmunk.Vect{})
	g.ball.body = chipmunk.BodyNew(ballMass, moment)
	g.space.AddBody(g.ball.body)
	g.ball.shape = chipmunk.CircleShapeNew(g.ball.body, ballRadius, chipmunk.Vect{})
	g.ball.shape.SetLayers(normLayer)
	g.ball.shape.SetElasticity(0.9)
	g.ball.shape.SetFriction(0.1)
	g.space.AddShape(g.ball.shape)
}

func (g *Gol) Run() {
	go g.run()
	go g.sim()
	g.Gordian.Run()
}

func (g *Gol) run() {
	for {
		select {
		case client := <-g.Control:
			g.clientCtrl(client)
		case msg := <-g.InBox:
			g.handleMessage(&msg)
		case <-g.updateTimer:
			g.update()
		}
	}
}

func (g *Gol) sim() {
	for {
		<-g.simTimer

		g.Lock()

		g.space.Step(simTime.Seconds())
		g.handlePauses()
		g.handleGoals()

		g.Unlock()
	}
}

func (g *Gol) handlePauses() {
	// enable control if pause is ending
	for _, player := range g.players {
		if g.pauseTicks[player.team] == 1 {
			player.enableCursorJoint(true)
		}
	}
	// update pause countdown
	for i := range g.pauseTicks {
		if g.pauseTicks[i] > 0 {
			g.pauseTicks[i]--
		}
	}
}

func (g *Gol) handleGoals() {
	ballX := g.ball.body.Position().X
	if math.Abs(ballX) > fieldWidth/2 { // GOL!
		team := 0
		if ballX < 0 {
			team = 1
		}
		g.score[team]++
		if g.score[0] >= maxScore || g.score[1] >= maxScore {
			g.score[0], g.score[1] = 0, 0
		}
		g.kickoff(team)
	}
}

func (g *Gol) kickoff(team int) {
	otherTeam := 1 - team

	g.ball.body.SetPosition(chipmunk.Vect{})
	g.ball.body.SetVelocity(chipmunk.Vect{})
	for _, player := range g.players {
		player.place()
		player.body.SetVelocity(chipmunk.Vect{})
		player.enableCursorJoint(false) // disable control for a bit
	}
	// give the team that was scored on a little head start for "kickoff"
	g.pauseTicks[team] = int((pauseTime + headStartTime) / simTime)
	g.pauseTicks[otherTeam] = int(pauseTime / simTime)
}

func (g *Gol) clientCtrl(client *gordian.Client) {
	switch client.Ctrl {
	case gordian.Connect:
		g.connect(client)
	case gordian.Close:
		g.close(client)
	}
}

func (g *Gol) nextTeam() int {
	teamSize := []int{0, 0}
	for _, player := range g.players {
		teamSize[player.team]++
	}
	switch {
	case teamSize[0] < teamSize[1]:
		return 0
	case teamSize[0] > teamSize[1]:
		return 1
	default:
		return rand.Intn(2)
	}
}

func (g *Gol) addPlayer(id gordian.ClientId) *player {
	player := &player{id: id, team: g.nextTeam()}

	moment := chipmunk.MomentForCircle(playerMass, 0, playerRadius, chipmunk.Vect{})
	player.body = chipmunk.BodyNew(playerMass, moment)
	g.space.AddBody(player.body)

	player.shape = chipmunk.CircleShapeNew(player.body, playerRadius, chipmunk.Vect{})
	player.shape.SetLayers(normLayer | goalLayer)
	player.shape.SetElasticity(0.9)
	player.shape.SetFriction(0.1)
	g.space.AddShape(player.shape)

	player.cursorBody = chipmunk.BodyNew(math.Inf(0), math.Inf(0))
	player.cursorJoint = chipmunk.PivotJointNew2(player.cursorBody, player.body,
		chipmunk.Vect{}, chipmunk.Vect{})
	player.cursorJoint.SetMaxForce(1000.0)
	player.enableCursorJoint(true)

	g.players[player.id] = player

	return player
}

func (g *Gol) removePlayer(id gordian.ClientId) {
	player, ok := g.players[id]
	if !ok {
		return
	}
	player.enableCursorJoint(false)
	player.cursorJoint.Free()
	g.space.RemoveBody(player.body)
	g.space.RemoveShape(player.shape)
	player.body.Free()
	player.shape.Free()
	player.cursorBody.Free()

	delete(g.players, id)
}

func (g *Gol) connect(client *gordian.Client) {
	g.curId++

	client.Id = g.curId
	client.Ctrl = gordian.Register
	g.Control <- client
	client = <-g.Control
	if client.Ctrl != gordian.Establish {
		return
	}

	g.Lock()

	player := g.addPlayer(client.Id)
	player.place()

	g.Unlock()

	data := configMsg{
		FieldWidth:   fieldWidth,
		FieldHeight:  fieldHeight,
		GoalSize:     goalSize,
		PlayerRadius: playerRadius,
		BallRadius:   ballRadius,
		Id:           fmt.Sprintf("%d", client.Id),
	}
	msg := gordian.Message{
		To:   client.Id,
		Type: "config",
		Data: data,
	}
	g.OutBox <- msg
}

func (g *Gol) close(client *gordian.Client) {
	g.Lock()
	defer g.Unlock()

	g.removePlayer(client.Id)
}

func (g *Gol) handleMessage(msg *gordian.Message) {
	g.Lock()
	defer g.Unlock()

	id := msg.From
	player, ok := g.players[id]
	if !ok {
		return
	}
	switch msg.Type {
	case "player":
		state := &Player{}
		err := msg.Unmarshal(state)
		if err != nil {
			return
		}
		player.cursorBody.SetPosition(state.Pos)
	}
}

func (g *Gol) update() {
	g.Lock()

	state := stateMsg{
		Players: map[string]Player{},
		Ball:    Ball{g.ball.body.Position()},
		Score:   g.score,
	}
	for i, player := range g.players {
		state.Players[fmt.Sprintf("%d", i)] = Player{
			Pos:  player.body.Position(),
			Team: player.team,
		}
	}

	g.Unlock()

	msg := gordian.Message{
		Type: "state",
		Data: state,
	}
	for id := range g.players {
		msg.To = id
		g.OutBox <- msg
	}
}
