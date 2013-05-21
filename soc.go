package soc

import (
	"github.com/ianremmler/gochipmunk/chipmunk"
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
	fieldWidth    = 1000
	fieldHeight   = 500
	edgeSize      = 16
	goalSize      = 200
	playerRadius  = 10
	playerMass    = 1
	ballRadius    = 20
	ballMass      = 0.1
)

const (
	normType = 1 << iota
	goalType
)

type player struct {
	id          gordian.ClientId
	team        int
	body        chipmunk.Body
	shape       chipmunk.Shape
	cursorBody  chipmunk.Body
	cursorJoint chipmunk.Constraint
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

type Soc struct {
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

func NewSoc() *Soc {
	s := &Soc{
		players:     map[gordian.ClientId]*player{},
		score:       []int{0, 0},
		pauseTicks:  []int{0, 0},
		simTimer:    time.Tick(simTime),
		updateTimer: time.Tick(updateTime),
		Gordian:     gordian.New(24),
	}
	s.setup()
	return s
}

func (s *Soc) setup() {
	s.space = chipmunk.SpaceNew()
	s.space.SetDamping(0.1)
	hfw, hfh, hgs := 0.5*fieldWidth, 0.5*fieldHeight, 0.5*goalSize
	sidePts := []chipmunk.Vect{{-hfw, hgs}, {-hfw, hfh}, {hfw, hfh}, {hfw, hgs}}
	numSideSegs := len(sidePts) - 1
	for i := 0; i < 2; i++ {
		sign := 2*float64(i) - 1
		for j := 0; j < numSideSegs; j++ {
			p0, p1 := sidePts[j], sidePts[j+1]
			p0.Y *= sign
			p1.Y *= sign
			fieldSeg := chipmunk.SegmentShapeNew(s.space.StaticBody(), p0, p1, 0.5*edgeSize)
			fieldSeg.SetLayers(normType)
			fieldSeg.SetElasticity(1.0)
			fieldSeg.SetFriction(1.0)
			s.space.AddShape(fieldSeg)
		}
		p0, p1 := chipmunk.Vect{sign * hfw, -hgs}, chipmunk.Vect{sign * hfw, hgs}
		goal := chipmunk.SegmentShapeNew(s.space.StaticBody(), p0, p1, 0.5*edgeSize)
		goal.SetLayers(goalType)
		goal.SetElasticity(1.0)
		goal.SetFriction(1.0)
		s.space.AddShape(goal)
	}
	moment := chipmunk.MomentForCircle(ballMass, 0, ballRadius, chipmunk.Origin())
	s.ball.body = chipmunk.BodyNew(ballMass, moment)
	s.space.AddBody(s.ball.body)
	s.ball.shape = chipmunk.CircleShapeNew(s.ball.body, ballRadius, chipmunk.Origin())
	s.ball.shape.SetLayers(normType)
	s.ball.shape.SetElasticity(0.9)
	s.ball.shape.SetFriction(0.1)
	s.space.AddShape(s.ball.shape)
	s.space.SetUserData(s)
}

func (s *Soc) Run() {
	go s.run()
	go s.sim()
	s.Gordian.Run()
}

func (s *Soc) run() {
	for {
		select {
		case client := <-s.Control:
			s.clientCtrl(client)
		case msg := <-s.InBox:
			s.handleMessage(&msg)
		case <-s.updateTimer:
			s.update()
		}
	}
}

func (s *Soc) sim() {
	for {
		<-s.simTimer

		s.mu.Lock()

		s.space.Step(float64(simTime) / float64(time.Second))

		// enable control if pause is ending
		for _, player := range s.players {
			if s.pauseTicks[player.team] == 1 {
				s.space.AddConstraint(player.cursorJoint)
			}
		}
		// update pause countdown
		for i := range s.pauseTicks {
			if s.pauseTicks[i] > 0 {
				s.pauseTicks[i]--
			}
		}
		// check for goals
		ballX := s.ball.body.Position().X
		if math.Abs(ballX) > fieldWidth/2 { // GOL!
			team := 0
			if ballX < 0 {
				team = 1
			}
			otherTeam := 1 - team
			s.score[team]++
			s.ball.body.SetPosition(chipmunk.Vect{})
			s.ball.body.SetVelocity(chipmunk.Vect{})
			// disable control for a bit
			for _, player := range s.players {
				player.body.SetPosition(playerPos(player.team))
				player.body.SetVelocity(chipmunk.Vect{})
				if s.pauseTicks[player.team] == 0 {
					s.space.RemoveConstraint(player.cursorJoint)
				}
			}
			// give the team that was scored on a little head start for "kickoff"
			s.pauseTicks[team] = int((pauseTime + headStartTime) / simTime)
			s.pauseTicks[otherTeam] = int(pauseTime / simTime)
		}

		s.mu.Unlock()
	}
}

func (s *Soc) clientCtrl(client *gordian.Client) {
	switch client.Ctrl {
	case gordian.Connect:
		s.connect(client)
	case gordian.Close:
		s.close(client)
	}
}

func (s *Soc) nextTeam() int {
	t0Size := 0
	for _, player := range s.players {
		if player.team == 0 {
			t0Size++
		}
	}
	diff := len(s.players) - 2*t0Size
	switch {
	case diff > 0:
		return 0
	case diff < 0:
		return 1
	default:
		return rand.Int() % 2
	}
}

func (s *Soc) connect(client *gordian.Client) {
	s.curId++

	client.Id = s.curId
	client.Ctrl = gordian.Register
	s.Control <- client
	client = <-s.Control
	if client.Ctrl != gordian.Establish {
		return
	}

	s.mu.Lock()

	player := &player{id: client.Id}
	moment := chipmunk.MomentForCircle(playerMass, 0, playerRadius, chipmunk.Origin())
	player.body = chipmunk.BodyNew(playerMass, moment)
	player.body.SetUserData(client.Id)
	s.space.AddBody(player.body)
	player.shape = chipmunk.CircleShapeNew(player.body, playerRadius, chipmunk.Origin())
	player.shape.SetLayers(normType)
	player.shape.SetElasticity(0.9)
	player.shape.SetFriction(0.1)
	s.space.AddShape(player.shape)

	player.cursorBody = chipmunk.BodyNew(math.Inf(0), math.Inf(0))
	player.cursorJoint = chipmunk.PivotJointNew2(player.cursorBody, player.body,
		chipmunk.Vect{}, chipmunk.Vect{})
	player.cursorJoint.SetMaxForce(1000.0)
	s.space.AddConstraint(player.cursorJoint)
	player.team = s.nextTeam()
	player.body.SetPosition(playerPos(player.team))

	s.players[player.id] = player

	s.mu.Unlock()

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
	s.OutBox <- msg
}

func (s *Soc) close(client *gordian.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	player, ok := s.players[client.Id]
	if !ok {
		return
	}
	s.space.RemoveConstraint(player.cursorJoint)
	player.cursorJoint.Free()
	s.space.RemoveBody(player.body)
	s.space.RemoveShape(player.shape)
	player.body.Free()
	player.shape.Free()
	player.cursorBody.Free()
	delete(s.players, client.Id)
}

func (s *Soc) handleMessage(msg *gordian.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := msg.From
	player, ok := s.players[id]
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

func (s *Soc) update() {
	s.mu.Lock()

	if s.score[0] > 99 || s.score[1] > 99 {
		s.score[0], s.score[1] = 0, 0
		for _, player := range s.players {
			player.body.SetPosition(chipmunk.Vect{})
		}
	}
	state := stateMsg{
		Players: map[string]Player{},
		Ball:    Ball{s.ball.body.Position()},
		Score:   s.score,
	}
	for i, player := range s.players {
		state.Players[fmt.Sprintf("%d", i)] = Player{
			Pos:  player.body.Position(),
			Team: player.team,
		}
	}

	s.mu.Unlock()

	msg := gordian.Message{
		Type: "state",
		Data: state,
	}
	for id := range s.players {
		msg.To = id
		s.OutBox <- msg
	}
}

func playerPos(team int) chipmunk.Vect {
	fw, fh := 0.5*fieldWidth, 0.5*fieldHeight
	pos := chipmunk.Vect{rand.Float64() * fw, rand.Float64()*(2*fh) - fh}
	if team == 0 {
		pos.X *= -1
	}
	gs := 0.5 * goalSize
	len := pos.Length()
	if len < gs {
		pos = pos.Div(len).Mul(gs)
	}
	return pos
}
