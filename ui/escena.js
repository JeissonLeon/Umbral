'use strict';
// Bosque nocturno en pixel art, pintado a 1/4 de resolución. La runa Ω del
// centro se llena de abajo arriba con el progreso real (window.setRune).
(function () {
  var C = document.getElementById('scene'), x = C.getContext('2d');
  var W = 0, H = 0, S = 4, fill = 0, target = 0, sky = null, motes = [];
  var still = matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Generador con semilla: el bosque es siempre el mismo.
  function rng(seed) { return function () { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; }; }

  // Ω de 15 × 17 celdas.
  var RUNE = [
    '....#######....',
    '..###.....###..',
    '.##.........##.',
    '##...........##',
    '##...........##',
    '##...........##',
    '##...........##',
    '##...........##',
    '.##.........##.',
    '.##.........##.',
    '..##.......##..',
    '..##.......##..',
    '...##.....##...',
    '...##.....##...',
    '#####.....#####',
    '#####.....#####',
    '...............'
  ];

  function tree(g, r, base, cx, h, color) {
    var w = 3 + Math.floor(r() * 4);
    g.fillStyle = color;
    g.fillRect(cx - Math.floor(w / 2), base - h, w, h);
    var tiers = 3 + Math.floor(r() * 3), top = base - h - 8;
    for (var i = 0; i < tiers; i++) {
      var half = 5 + i * 5 + Math.floor(r() * 3), y = top + i * 9;
      for (var j = 0; j < 8; j++) g.fillRect(cx - Math.round(half * (j + 1) / 8), y + j, Math.round(half * (j + 1) / 4), 1);
    }
  }

  function paintSky() {
    sky = document.createElement('canvas');
    sky.width = W; sky.height = H;
    var g = sky.getContext('2d'), r = rng(20113), i;
    var bands = ['#07130f', '#0a1b18', '#0d2420', '#112e27', '#163a2e', '#1c4636'];
    for (i = 0; i < bands.length; i++) { g.fillStyle = bands[i]; g.fillRect(0, Math.floor(H * i / bands.length), W, Math.ceil(H / bands.length) + 1); }
    g.fillStyle = '#9fb7a6';
    for (i = 0; i < W / 4; i++) g.fillRect(Math.floor(r() * W), Math.floor(r() * H * .55), 1, 1);
    var layers = [['#123026', .58, 16, 40], ['#0c241d', .68, 22, 50], ['#071711', .8, 30, 58]];
    layers.forEach(function (l) {
      var base = Math.floor(H * l[1]) + 30, step = l[2];
      for (var cx = -10; cx < W + 10; cx += step + Math.floor(r() * step)) tree(g, r, base, cx, l[3] + Math.floor(r() * 16), l[0]);
      g.fillStyle = l[0];
      g.fillRect(0, base, W, H - base);
    });
    g.fillStyle = '#050f0b';
    g.fillRect(0, Math.floor(H * .86), W, H);
  }

  function resize() {
    W = Math.ceil(innerWidth / S); H = Math.ceil(innerHeight / S);
    C.width = W; C.height = H;
    paintSky();
    var r = rng(77);
    motes = [];
    for (var i = 0; i < 24; i++) motes.push({ x: r() * W, y: r() * H, v: .04 + r() * .08, p: r() * 6 });
    draw(0);
  }

  function rune() {
    var cell = Math.max(2, Math.floor(H / 34));
    var w = RUNE[0].length * cell, h = RUNE.length * cell;
    // A la izquierda del panel, en el claro del bosque.
    var ox = Math.floor(Math.min(W * .3, W / 2 - w / 2) - w / 2), oy = Math.floor(H * .5 - h / 2);
    var filled = Math.round(RUNE.length * fill);
    RUNE.forEach(function (row, j) {
      var lit = RUNE.length - j <= filled;
      for (var i = 0; i < row.length; i++) {
        if (row[i] !== '#') continue;
        // Halo pegado a la forma: un bloque más grande y tenue detrás de cada celda.
        x.globalAlpha = lit ? .18 : .06;
        x.fillStyle = '#8ff0c6';
        x.fillRect(ox + i * cell - cell, oy + j * cell - cell, cell * 3, cell * 3);
        x.globalAlpha = 1;
      }
    });
    RUNE.forEach(function (row, j) {
      var lit = RUNE.length - j <= filled;
      x.fillStyle = lit ? '#8ff0c6' : '#2f5a48';
      for (var i = 0; i < row.length; i++) if (row[i] === '#') x.fillRect(ox + i * cell, oy + j * cell, cell, cell);
    });
  }

  function draw(t) {
    x.drawImage(sky, 0, 0);
    rune();
    x.fillStyle = '#8ff0c6';
    motes.forEach(function (m) {
      var y = (m.y - t * m.v * .03) % H; if (y < 0) y += H;
      x.globalAlpha = .35 + .35 * Math.sin(t * .001 + m.p);
      x.fillRect(Math.floor(m.x + Math.sin(t * .0006 + m.p) * 3), Math.floor(y), 1, 1);
    });
    x.globalAlpha = 1;
  }

  // A 8 cuadros por segundo: es un fondo y no debe gastar CPU junto al juego.
  function loop() {
    fill += (target - fill) * .25;
    draw(performance.now());
    setTimeout(loop, document.hidden ? 1000 : 125);
  }

  window.setRune = function (v) { target = Math.max(0, Math.min(1, v)); if (still) { fill = target; draw(0); } };
  addEventListener('resize', resize);
  resize();
  if (!still) loop();

  // Logotipo: letras de 5 × 7 con Ω propia.
  var G = {
    S: '.####|#....|#....|.###.|....#|....#|####.', Y: '#...#|#...#|.#.#.|..#..|..#..|..#..|..#..',
    L: '#....|#....|#....|#....|#....|#....|#####', V: '#...#|#...#|#...#|#...#|.#.#.|.#.#.|..#..',
    A: '.###.|#...#|#...#|#####|#...#|#...#|#...#', N: '#...#|##..#|##..#|#.#.#|#..##|#..##|#...#',
    O: '.###.|#...#|#...#|#...#|.#.#.|.#.#.|##.##', R: '####.|#...#|#...#|####.|#.#..|#..#.|#...#',
    3: '####.|....#|....#|.###.|....#|....#|####.', ' ': '..|..|..|..|..|..|..'
  };
  var word = 'SYLVANOR 3', L = document.getElementById('logo'), lx = L.getContext('2d');
  var total = 0; word.split('').forEach(function (c) { total += G[c].split('|')[0].length + 1; });
  L.width = total + 1; L.height = 9;
  L.style.width = (L.width * 56 / 9) + 'px';
  var pen = 0;
  word.split('').forEach(function (c) {
    G[c].split('|').forEach(function (row, j) {
      for (var i = 0; i < row.length; i++) {
        if (row[i] !== '#') continue;
        lx.fillStyle = '#000'; lx.fillRect(pen + i + 1, j + 1, 1, 1);
      }
    });
    G[c].split('|').forEach(function (row, j) {
      for (var i = 0; i < row.length; i++) {
        if (row[i] !== '#') continue;
        lx.fillStyle = c === 'O' ? '#8ff0c6' : '#e9e6d2'; lx.fillRect(pen + i, j, 1, 1);
      }
    });
    pen += G[c].split('|')[0].length + 1;
  });
})();
