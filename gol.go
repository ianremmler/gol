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
	hfw, hfh := 0.5*fieldWidth-playerRadius, 0.5*fieldHeight-playerRadius
	pos := chipmunk.Vect{rand.Float64() * hfw, rand.Float64()*(2*hfh) - hfh}
	if p.team == 0 {
		pos.X = -pos.X
	}
	minDist := 0.25*fieldHeight + playerRadius
	len := pos.Length()
	if len < minDist {
		pos = pos.Div(len).Mul(minDist)
	}
	p.body.SetPosition(pos)
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
	mu          sync.Mutex
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
		sign := 2*float64(i) - 1
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
	moment := chipmunk.MomentForCircle(ballMass, 0, ballRadius, chipmunk.Origin())
	g.ball.body = chipmunk.BodyNew(ballMass, moment)
	g.space.AddBody(g.ball.body)
	g.ball.shape = chipmunk.CircleShapeNew(g.ball.body, ballRadius, chipmunk.Origin())
	g.ball.shape.SetLayers(normLayer)
	g.ball.shape.SetElasticity(0.9)
	g.ball.shape.SetFriction(0.1)
	g.space.AddShape(g.ball.shape)
	g.space.SetUserData(g)
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

		g.mu.Lock()

		g.space.Step(float64(simTime) / float64(time.Second))

		// enable control if pause is ending
		for _, player := range g.players {
			if g.pauseTicks[player.team] == 1 {
				g.space.AddConstraint(player.cursorJoint)
			}
		}
		// update pause countdown
		for i := range g.pauseTicks {
			if g.pauseTicks[i] > 0 {
				g.pauseTicks[i]--
			}
		}
		// check for goals
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

		g.mu.Unlock()
	}
}

func (g *Gol) kickoff(team int) {
	otherTeam := 1 - team

	g.ball.body.SetPosition(chipmunk.Vect{})
	g.ball.body.SetVelocity(chipmunk.Vect{})
	for _, player := range g.players {
		player.place()
		player.body.SetVelocity(chipmunk.Vect{})
		if g.pauseTicks[player.team] == 0 {
			// disable control for a bit
			g.space.RemoveConstraint(player.cursorJoint)
		}
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
	t0Size := 0
	for _, player := range g.players {
		if player.team == 0 {
			t0Size++
		}
	}
	diff := len(g.players) - 2*t0Size
	switch {
	case diff > 0:
		return 0
	case diff < 0:
		return 1
	default:
		return rand.Int() % 2
	}
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

	g.mu.Lock()

	player := &player{id: client.Id}
	moment := chipmunk.MomentForCircle(playerMass, 0, playerRadius, chipmunk.Origin())
	player.body = chipmunk.BodyNew(playerMass, moment)
	player.body.SetUserData(client.Id)
	g.space.AddBody(player.body)
	player.shape = chipmunk.CircleShapeNew(player.body, playerRadius, chipmunk.Origin())
	player.shape.SetLayers(normLayer | goalLayer)
	player.shape.SetElasticity(0.9)
	player.shape.SetFriction(0.1)
	g.space.AddShape(player.shape)

	player.cursorBody = chipmunk.BodyNew(math.Inf(0), math.Inf(0))
	player.cursorJoint = chipmunk.PivotJointNew2(player.cursorBody, player.body,
		chipmunk.Vect{}, chipmunk.Vect{})
	player.cursorJoint.SetMaxForce(1000.0)
	g.space.AddConstraint(player.cursorJoint)
	player.team = g.nextTeam()
	player.place()

	g.players[player.id] = player

	g.mu.Unlock()

	data := configMsg{
		FieldWidth:   fieldWidth,
		FieldHeight:  fieldHeight,
		GoalSize:     goalSize,
		PlayerRadius: playerRadius,
		BallRadius:   ballRadius,
		Id:           fmt.Sprintf("%d", player.id),
	}
	msg := gordian.Message{
		To:   player.id,
		Type: "config",
		Data: data,
	}
	g.OutBox <- msg
}

func (g *Gol) close(client *gordian.Client) {
	g.mu.Lock()
	defer g.mu.Unlock()

	player, ok := g.players[client.Id]
	if !ok {
		return
	}
	g.space.RemoveConstraint(player.cursorJoint)
	player.cursorJoint.Free()
	g.space.RemoveBody(player.body)
	g.space.RemoveShape(player.shape)
	player.body.Free()
	player.shape.Free()
	player.cursorBody.Free()
	delete(g.players, client.Id)
}

func (g *Gol) handleMessage(msg *gordian.Message) {
	g.mu.Lock()
	defer g.mu.Unlock()

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
	g.mu.Lock()

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

	g.mu.Unlock()

	msg := gordian.Message{
		Type: "state",
		Data: state,
	}
	for id := range g.players {
		msg.To = id
		g.OutBox <- msg
	}
}
