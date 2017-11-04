var players = {};
var localState = { pos: { x: 0.0, y: 0.0 } };
var ball;
var config;
var score = [ 0, 0 ];
var teamColor = ["red", "blue"];
var field = document.getElementById("field");
var ws = new WebSocket("ws://" + window.location.host + "/gol/");

window.addEventListener("resize", resizeField);
field.addEventListener("mousemove", mousePos);
ws.addEventListener("message", handleMessage);

function handleMessage(evt) {
	msg = JSON.parse(evt.data);
	switch (msg.type) {
	case "config":
		setup(msg.data);
		break;
	case "state":
		updateGameState(msg.data);
		draw();
		sendLocalState();
		break;
	default:
		break;
	}
};

function setup(conf) {
	config = conf;
	resizeField();
}

function resizeField() {
	var w = window.innerWidth;
	var h = window.innerHeight;
	var fieldAspRat = config.FieldWidth / config.FieldHeight;
	var winAspRat = w / h;
	if (winAspRat > fieldAspRat) {
		w *= fieldAspRat / winAspRat;
	} else {
		h *= winAspRat / fieldAspRat ;
	}
	field.width = w;
	field.height = h;
}

function draw() {
	var ctx = field.getContext("2d");
	var cw = field.width;
	var ch = field.height;
	var fw = config.FieldWidth;
	var fh = config.FieldHeight;
	var gs = config.GoalSize;
	var er = config.EdgeRadius;

	ctx.save();
	ctx.translate(0.5 * cw, 0.5 * ch);
	ctx.scale(cw / (fw + 2.0 * er), -ch / (fh + 2.0 * er));

	// field
	ctx.lineWidth = 2.0 * er;
	ctx.fillStyle = "green";
	ctx.fillRect(-0.5 * fw, -0.5 * fh, fw, fh);
	ctx.strokeStyle = "white";

	// score
	ctx.save();
	ctx.fillStyle = "white";
	ctx.textAlign = "center";
	ctx.textBaseline = "middle";
	ctx.globalAlpha = 0.25;
	ctx.scale(1.0, -1.0);
	ctx.font = (0.25 * fh) + "px sans";
	for (i = 0; i < 2; i++) {
		var side = 2 * i - 1;
		ctx.fillText(score[i], side * 0.25 * fw, 0.0);
	}
	ctx.restore();

	// touch
	ctx.strokeRect(-0.5 * fw, -0.5 * fh, fw, fh);

	// center circle
	ctx.beginPath();
	ctx.arc(0.0, 0.0, 0.5 * gs, 0.0, 2.0 * Math.PI);
	ctx.stroke();

	// midfield
	ctx.beginPath();
	ctx.moveTo(0.0, -0.5 * fh);
	ctx.lineTo(0.0, 0.5 * fh);
	ctx.stroke();

	// penalty box
	for (i = 0; i < 2; i++) {
		var side = 2 * i - 1;
		ctx.strokeRect(side * 0.5 * fw, -0.5 * gs, side * -0.5 * gs, gs);
	}

	// goal line
	for (i = 0; i < 2; i++) {
		var side = 2 * i - 1;
		ctx.strokeStyle = teamColor[i];
		ctx.beginPath();
		ctx.moveTo(side * 0.5 * fw, -0.5 * (gs - 2 * er));
		ctx.lineTo(side * 0.5 * fw, 0.5 * (gs - 2 * er));
		ctx.stroke();
	}

	ctx.lineWidth = 1;

	// players
	for (var id in players) {
		ctx.strokeStyle = "black";
		ctx.fillStyle = teamColor[players[id].Team];
		ctx.beginPath();
		var pos = players[id].Pos;
		var rad = config.PlayerRadius;
		ctx.arc(pos.X, pos.Y, rad, 0.0, 2.0 * Math.PI);
		ctx.fill();
		ctx.stroke();
		if (id == config.Id) {
			ctx.fillStyle = "yellow";
			ctx.beginPath();
			ctx.arc(pos.X, pos.Y, 0.5 * rad, 0.0, 2.0 * Math.PI);
			ctx.fill();
		}
	}

	// ball
	ctx.fillStyle = "white";
	ctx.beginPath();
	ctx.arc(ball.Pos.X, ball.Pos.Y, config.BallRadius, 0.0, 2.0 * Math.PI);
	ctx.fill();
	ctx.stroke();

	// outline
	ctx.strokeRect(-0.5 * fw - er, -0.5 * fh - er, fw + 2 * er, fh + 2 * er);

	ctx.restore();
}

function updateGameState(gameState) {
	players = gameState.Players;
	ball = gameState.Ball;
	score = gameState.Score;
}

function sendLocalState() {
	ws.send(JSON.stringify({ type: "player", data: localState }));
}

function mousePos(evt) {
	fieldPos(evt.clientX, evt.clientY);
}

function fieldPos(px, py) {
	var bb = field.getBoundingClientRect();
	var cx = px - bb.left;
	var cy = py - bb.top;
	var cw = field.width;
	var ch = field.height;
	var fw = config.FieldWidth;
	var fh = config.FieldHeight;
	var er = config.EdgeRadius;

	localState.pos.x = clamp((cx / cw - 0.5) * (fw + 2.0 * er), -0.5 * fw, 0.5 * fw);
	localState.pos.y = clamp(-(cy / ch - 0.5) * (fh + 2.0 * er), -0.5 * fh, 0.5 * fh);
}

function clamp(val, min, max) {
	if (val < min) {
		val = min;
	} else if (val > max) {
		val = max;
	}
	return val;
}
