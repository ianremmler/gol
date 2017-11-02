package gol

import (
	"github.com/ianremmler/gordian"
	"github.com/jakecoffman/cp"

	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

const (
	frameRate    = 30
	simTime      = time.Second / (2 * frameRate)
	updateTime   = time.Second / frameRate
	pauseTime    = time.Second
	maxScore     = 10
	fieldWidth   = 1000
	fieldHeight  = 500
	edgeRadius   = 5
	goalSize     = 200
	playerRadius = 15
	playerMass   = 1
	ballRadius   = 10
	ballMass     = 0.05
)

const (
	ballType = iota + 1
)

const (
	circleCat = 1 << iota
	ballCat
	goalCat
	team0Cat
	team1Cat
)

type player struct {
	id          gordian.ClientId
	team        int
	shape       *cp.Shape
	cursorBody  *cp.Body
	cursorJoint *cp.Constraint
}

func (p *player) place() {
	widthRange := 0.5*fieldWidth - 2*playerRadius
	heightRange := 0.5*fieldHeight - playerRadius
	sign := float64(2*p.team - 1)
	pos := cp.Vector{
		(widthRange*rand.Float64() + playerRadius) * sign,
		heightRange * (2*rand.Float64() - 1),
	}
	minDist := 0.25*fieldHeight + playerRadius
	dist := pos.Length()
	if dist < minDist {
		pos = pos.Mult(minDist / dist)
	}
	p.shape.Body().SetPosition(pos)
}

func (p *player) enableCursorJoint(enable bool) {
	sp := p.shape.Space()
	isEnabled := sp.ContainsConstraint(p.cursorJoint)
	switch {
	case enable && !isEnabled:
		sp.AddConstraint(p.cursorJoint)
	case !enable && isEnabled:
		sp.RemoveConstraint(p.cursorJoint)
	}
}

type Player struct {
	Pos  cp.Vector
	Team int
}

type Ball struct {
	Pos cp.Vector
}

type configMsg struct {
	Id           string
	FieldWidth   float64
	FieldHeight  float64
	GoalSize     float64
	PlayerRadius float64
	BallRadius   float64
	EdgeRadius   float64
}

type stateMsg struct {
	Players map[string]Player
	Ball    Ball
	Score   []int
}

type Gol struct {
	players      map[gordian.ClientId]*player
	score        []int
	pauseTicks   int
	simTimer     <-chan time.Time
	updateTimer  <-chan time.Time
	curId        int
	ball         *cp.Shape
	centerCircle *cp.Shape
	space        *cp.Space
	sync.Mutex
	*gordian.Gordian
}

func New() *Gol {
	g := &Gol{
		players:     map[gordian.ClientId]*player{},
		score:       []int{0, 0},
		simTimer:    time.Tick(simTime),
		updateTimer: time.Tick(updateTime),
		Gordian:     gordian.New(frameRate), // buffer max 1 second
	}
	g.setup()
	return g
}

func (g *Gol) setup() {
	g.space = cp.NewSpace()
	g.space.SetDamping(0.25)
	hfw, hfh, hgs := 0.5*fieldWidth, 0.5*fieldHeight, 0.5*goalSize
	sidePts := []cp.Vector{{-hfw, hgs}, {-hfw, hfh}, {hfw, hfh}, {hfw, hgs}}
	numSideSegs := len(sidePts) - 1
	for i := 0; i < 2; i++ {
		sign := float64(2*i - 1)
		for j := 0; j < numSideSegs; j++ {
			p0, p1 := sidePts[j], sidePts[j+1]
			p0.Y *= sign
			p1.Y *= sign
			fieldSeg := cp.NewSegment(g.space.StaticBody, p0, p1, edgeRadius)
			fieldSeg.SetElasticity(1.0)
			fieldSeg.SetFriction(1.0)
			g.space.AddShape(fieldSeg)
		}
		p0, p1 := cp.Vector{sign * hfw, -hgs}, cp.Vector{sign * hfw, hgs}
		goal := cp.NewSegment(g.space.StaticBody, p0, p1, edgeRadius)
		goal.Filter.Categories = goalCat
		goal.SetElasticity(1.0)
		goal.SetFriction(1.0)
		g.space.AddShape(goal)
	}
	g.centerCircle = cp.NewCircle(g.space.StaticBody, 0.5*goalSize+edgeRadius, cp.Vector{})
	g.centerCircle.Filter.Categories = circleCat
	g.centerCircle.Filter.Mask = 0
	g.centerCircle.SetElasticity(1.0)
	g.centerCircle.SetFriction(1.0)
	g.space.AddShape(g.centerCircle)

	moment := cp.MomentForCircle(ballMass, 0, ballRadius, cp.Vector{})
	body := cp.NewBody(ballMass, moment)
	g.space.AddBody(body)
	g.ball = cp.NewCircle(body, ballRadius, cp.Vector{})
	g.ball.SetCollisionType(ballType)
	g.ball.Filter.Categories = ballCat
	g.ball.Filter.Mask &^= goalCat
	g.ball.SetElasticity(1.0)
	g.ball.SetFriction(1.0)
	g.space.AddShape(g.ball)

	g.space.NewWildcardCollisionHandler(ballType).BeginFunc =
		func(arb *cp.Arbiter, space *cp.Space, data interface{}) bool {
			g.centerCircle.Filter.Mask = 0
			return true
		}
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
	if g.pauseTicks == 1 {
		for _, player := range g.players {
			player.enableCursorJoint(true)
		}
	}
	// update pause countdown
	if g.pauseTicks > 0 {
		g.pauseTicks--
	}
}

func (g *Gol) handleGoals() {
	ballX := g.ball.Body().Position().X
	if math.Abs(ballX) > fieldWidth/2 { // GOL!
		team := 0
		if ballX < 0 {
			team = 1
		}
		g.score[team]++
		if g.score[0] >= maxScore || g.score[1] >= maxScore {
			g.score[0], g.score[1] = 0, 0
			team = rand.Intn(2)
		}
		g.kickoff(team)
	}
}

func (g *Gol) kickoff(team int) {
	g.ball.Body().SetPosition(cp.Vector{})
	g.ball.Body().SetVelocityVector(cp.Vector{})
	for _, player := range g.players {
		player.place()
		player.shape.Body().SetVelocityVector(cp.Vector{})
		player.enableCursorJoint(false) // disable control for a bit
	}
	if len(g.players) > 1 {
		g.centerCircle.Filter.Mask |= team0Cat << uint(team)
	}
	g.pauseTicks = int(pauseTime / simTime)
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

	moment := cp.MomentForCircle(playerMass, 0, playerRadius, cp.Vector{})
	body := cp.NewBody(playerMass, moment)
	g.space.AddBody(body)

	player.shape = cp.NewCircle(body, playerRadius, cp.Vector{})
	player.shape.Filter.Categories = team0Cat << uint(player.team)
	player.shape.SetElasticity(1.0)
	player.shape.SetFriction(1.0)
	g.space.AddShape(player.shape)

	player.cursorBody = cp.NewBody(math.Inf(0), math.Inf(0))
	player.cursorJoint =
		cp.NewPivotJoint2(player.cursorBody, body, cp.Vector{}, cp.Vector{})
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
	g.space.RemoveBody(player.shape.Body())
	g.space.RemoveShape(player.shape)

	delete(g.players, id)
	if len(g.players) < 2 {
		g.centerCircle.Filter.Mask = 0
	}
}

func (g *Gol) connect(client *gordian.Client) {
	client.Id = g.curId
	client.Ctrl = gordian.Register
	g.Control <- client
	client = <-g.Control
	if client.Ctrl != gordian.Establish {
		return
	}
	g.curId++

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
		EdgeRadius:   edgeRadius,
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
		Ball:    Ball{g.ball.Body().Position()},
		Score:   g.score,
	}
	for i, player := range g.players {
		state.Players[fmt.Sprintf("%d", i)] = Player{
			Pos:  player.shape.Body().Position(),
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
