var players = {};
var state = { Pos: { X: 0, Y: 0 } };
var stage;
var layer;
var ball;
var me;
var config;
var scoreboard = [];
var teamColor = ['red', 'blue'];

function setup(conf) {
	config = conf;
	stage = new Kinetic.Stage({
		container: 'container',
		width: config.FieldWidth,
		height: config.FieldHeight,
		scale: { x: 1, y: -1 },
		offset: { x: -config.FieldWidth / 2.0, y: config.FieldHeight / 2.0 }
	});
	layer = new Kinetic.Layer();

	layer.add(new Kinetic.Rect({
		x: -config.FieldWidth / 2,
		y: -config.FieldHeight / 2,
		width: config.FieldWidth,
		height: config.FieldHeight,
		fill: 'lightgreen',
		stroke: 'black',
		strokeWidth: 8
	}));

	for (var i = 0; i < 2; i++) {
		var side = 2 * i - 1;
		layer.add(new Kinetic.Line({
			points: [
				side * config.FieldWidth / 2, -config.GoalSize / 2,
				side * config.FieldWidth / 2, config.GoalSize / 2
			],
			stroke: 'white',
			strokeWidth: 8
		}));
	}

	ball = new Kinetic.Circle({
		radius: config.BallRadius,
		fill: 'white',
		stroke: 'black',
		strokeWidth: 4
	});
	layer.add(ball);

	me = new Kinetic.Circle({
		x: 0,
		y: 0,
		radius: 4,
		fill: 'yellow'
	});
	layer.add(me);

	for (i = 0; i < 2; i++) {
		var text = new Kinetic.Text({
			fontSize: 72,
			fontFamily: 'monospace',
			x: (config.FieldWidth / 2 - 50) * (2 * i - 1) - 50,
			y: -config.FieldWidth / 2 + 100,
			width: 100,
			height: 200,
			text: '0',
			align: 'center',
			stroke: 'gray',
			fill: teamColor[i],
			scale: { x: 1, y: -1 }
		});
		scoreboard.push(text);
		layer.add(text);
	}

	stage.add(layer);
	anim();
}

function newPlayer(team) {
	var color = teamColor[team];
	var player = new Kinetic.Circle({
		radius: config.PlayerRadius,
		fill: color,
		strokeWidth: 4
	});
	return player;
}

var ws = new WebSocket("ws://" + window.location.host + "/soc/");
ws.onmessage = function(evt) {
	msg = JSON.parse(evt.data);
	switch (msg.type) {
	case "config":
		setup(msg.data);
		break;
	case "state":
		updatePlayers(msg.data.Players);
		updateBall(msg.data.Ball);
		updateScore(msg.data.Score);
		sendState();
		break;
	default:
		break;
	}
};

function updatePlayers(curPlayers) {
	for (var id in curPlayers) {
		if (!(id in players)) {
			var p = newPlayer(curPlayers[id].Team);
			players[id] = p;
			layer.add(p);
		}
		var x = curPlayers[id].Pos.X;
		var y = curPlayers[id].Pos.Y;
		players[id].setPosition(x, y);
		if (id == config.Id) {
			me.setPosition(x, y);
			me.moveToTop();
		}
	}
	for (id in players) {
		if (!(id in curPlayers)) {
			players[id].remove();
			delete players[id];
		}
	}
}

function updateBall(bal) {
	ball.setPosition(bal.Pos.X, bal.Pos.Y);
}

function updateScore(score) {
	for (var idx in score) {
		scoreboard[idx].setText(score[idx]);
	}
}

function sendState() {
	var msg = {
		type: 'player',
		data: state
	};
	ws.send(JSON.stringify(msg));
}

function anim() {
	requestAnimationFrame(anim);
	stage.draw();
	var pos = stage.getPointerPosition();
	if (pos) {
		state.Pos = {
		   	X: pos.x - config.FieldWidth / 2,
		   	Y: config.FieldHeight / 2 - pos.y
	   	};
	}
}
