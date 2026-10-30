'use strict';
(function () {
  var token = new URLSearchParams(location.search).get('t') || '';
  var $ = function (s) { return document.querySelector(s); };
  var state = null;
  var stage = '';
  var progress = null;

  function api(path, body) {
    return fetch('/api/' + path, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { 'X-Umbral-Token': token, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body)
    }).then(function (r) {
      return r.json().then(function (j) {
        if (!r.ok) throw new Error(j.error || ('Error ' + r.status));
        return j;
      });
    });
  }

  // Pestañas
  var tabs = document.querySelectorAll('.nav button');
  function show(v) {
    tabs.forEach(function (t) { t.setAttribute('aria-selected', t.dataset.view === v ? 'true' : 'false'); });
    ['play', 'mods', 'settings'].forEach(function (id) { $('#view-' + id).hidden = id !== v; });
  }
  tabs.forEach(function (t) { t.addEventListener('click', function () { show(t.dataset.view); }); });

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function render() {
    if (!state) return;
    var s = state, acc = s.cuenta, busy = s.actividad;

    // Cuenta
    $('#acc-name').textContent = acc ? acc.nombre : 'Sin sesión';
    $('#acc-sub').textContent = acc ? 'Cuenta de Microsoft' : 'Inicia sesión para jugar';
    var av = $('#avatar');
    av.textContent = acc ? '' : '?';
    av.style.backgroundImage = acc ? 'url("https://mc-heads.net/avatar/' + encodeURIComponent(acc.uuid) + '/56")' : '';
    $('#acc-detail').textContent = acc ? ('Sesión iniciada como ' + acc.nombre + '.') : 'Sin sesión.';
    $('#logout').hidden = !acc;

    // Versiones
    $('#v-mc').textContent = s.modpack.minecraft;
    $('#v-forge').textContent = s.modpack.forge;
    $('#v-pack').textContent = s.modpack.version;
    $('#news-ver').textContent = s.modpack.version;
    var news = $('#news'); news.innerHTML = '';
    (s.modpack.novedades || []).forEach(function (n) { news.appendChild(el('li', '', n)); });

    // Título y botón principal
    var title = $('#play-title'), lead = $('#play-lead'), btn = $('#play');
    $('#cancel-login').hidden = busy !== 'sesion';
    if (!s.login_configurado) {
      title.textContent = 'Falta configurar';
      lead.textContent = 'Este launcher se compiló sin el ID de aplicación de Microsoft, así que no puede iniciar sesión. Quien lo distribuye debe compilarlo con ese ID.';
      btn.textContent = 'Iniciar sesión'; btn.disabled = true;
    } else if (busy === 'sesion') {
      title.textContent = 'Inicia sesión';
      lead.textContent = 'Se abrió la página de Microsoft en tu navegador. Inicia sesión allí y vuelve aquí.';
      btn.textContent = 'Esperando a Microsoft…'; btn.disabled = true;
    } else if (!acc) {
      title.textContent = 'Bienvenido';
      lead.textContent = 'Inicia sesión con la cuenta de Microsoft con la que compraste Minecraft: Java Edition.';
      btn.textContent = 'Iniciar sesión con Microsoft'; btn.disabled = false;
    } else if (busy === 'preparando') {
      title.textContent = 'Preparando';
      lead.textContent = s.instalado ? 'Comprobando archivos y actualizaciones.' : 'Primera instalación: se descargan Minecraft, Java, Forge y los mods. Tarda unos minutos.';
      btn.textContent = 'Preparando…'; btn.disabled = true;
    } else if (busy === 'jugando') {
      title.textContent = 'Jugando';
      lead.textContent = 'Minecraft está abierto. Puedes dejar el launcher en segundo plano.';
      btn.textContent = 'Jugando'; btn.disabled = true;
    } else {
      title.textContent = 'Listo para jugar';
      lead.textContent = s.ajustes.servidor ? ('Al pulsar Jugar entras directo a ' + s.ajustes.servidor + '.') : 'Al pulsar Jugar se abre el menú principal de Minecraft.';
      btn.textContent = s.instalado ? 'Jugar' : 'Instalar y jugar'; btn.disabled = false;
    }

    // Progreso
    var bar = $('#bar'), pr = $('#progress'), st = $('#status');
    if (busy === 'preparando') {
      if (progress && progress.total > 0 && progress.hecho < progress.total) {
        pr.classList.remove('busy');
        bar.style.width = Math.round(progress.hecho / progress.total * 100) + '%';
        st.textContent = progress.etiqueta + ': ' + progress.hecho + ' de ' + progress.total;
      } else {
        pr.classList.add('busy');
        st.textContent = stage || 'Preparando…';
      }
    } else {
      pr.classList.remove('busy');
      bar.style.width = busy === 'jugando' ? '100%' : '0';
      st.textContent = busy === 'jugando' ? 'Minecraft en marcha' : (s.instalado ? 'Instalado' : 'Aún no instalado');
    }
    var err = $('#error');
    err.hidden = !s.error;
    err.textContent = capital(s.error || '');

    renderServer(s.servidor, s.ajustes.servidor);
    renderMods(s);

    // Ajustes (sin pisar lo que el usuario está escribiendo)
    if (document.activeElement !== $('#server')) $('#server').value = s.ajustes.servidor || '';
    if (document.activeElement !== $('#ram')) { $('#ram').value = Math.round(s.ajustes.memoria_mb / 1024); ram(); }
    $('#datadir').textContent = s.carpeta;
    $('#foot').textContent = 'Umbral Launcher ' + s.version_launcher + ' · ' + s.modpack.nombre + ' ' + s.modpack.version;
  }

  function renderServer(st, addr) {
    var body = $('#srv-body'); body.innerHTML = '';
    if (!addr) {
      body.appendChild(el('p', 'note', 'Sin servidor configurado. Añádelo en Ajustes para entrar directo al jugar.'));
      return;
    }
    var dl = el('dl');
    function row(k, v) { dl.appendChild(el('dt', '', k)); var d = el('dd'); if (typeof v === 'string') d.textContent = v; else d.appendChild(v); dl.appendChild(d); }
    row('Dirección', addr);
    if (!st) {
      row('Estado', 'Consultando…');
    } else if (!st.en_linea) {
      var off = el('span'); off.appendChild(el('span', 'dot off')); off.appendChild(document.createTextNode('Sin conexión'));
      row('Estado', off);
      if (st.error) row('Detalle', st.error);
    } else {
      var on = el('span'); on.appendChild(el('span', 'dot')); on.appendChild(document.createTextNode('En línea'));
      row('Estado', on);
      row('Jugadores', st.jugadores + ' / ' + st.maximo);
      row('Versión', st.version);
      row('Latencia', st.latencia_ms + ' ms');
    }
    body.appendChild(dl);
    if (st && st.motd) body.appendChild(el('p', 'motd', st.motd));
  }

  function renderMods(s) {
    var tb = $('#extras'); tb.innerHTML = '';
    var active = 0;
    (s.extras || []).forEach(function (m) {
      if (m.activo) active++;
      var tr = el('tr');
      var c1 = el('td'); c1.appendChild(el('div', 'name', m.nombre)); c1.appendChild(el('div', 'why', m.descripcion));
      var c2 = el('td'); c2.appendChild(el('span', 'chip', m.lado));
      var c3 = el('td'); var sw = el('label', 'switch'); var cb = el('input');
      cb.type = 'checkbox'; cb.checked = m.activo; cb.disabled = !!s.actividad;
      cb.setAttribute('aria-label', 'Activar ' + m.nombre);
      cb.addEventListener('change', function () {
        api('extra', { archivo: m.archivo, activo: cb.checked }).catch(function (e) { cb.checked = !cb.checked; alertBox(e.message); });
      });
      sw.appendChild(cb); c3.appendChild(sw);
      tr.appendChild(c1); tr.appendChild(c2); tr.appendChild(c3);
      tb.appendChild(tr);
    });
    $('#extrachip').textContent = active + (active === 1 ? ' activo' : ' activos');
    $('#modcount').textContent = String((s.mods || []).length + active);

    var base = $('#base'); base.innerHTML = '';
    (s.mods || []).forEach(function (m) {
      var tr = el('tr');
      var c1 = el('td'); c1.appendChild(el('div', 'name', m.nombre));
      var c2 = el('td'); c2.appendChild(el('span', 'chip', m.lado));
      tr.appendChild(c1); tr.appendChild(c2); tr.appendChild(el('td', 'why', m.descripcion));
      base.appendChild(tr);
    });
  }

  function capital(t) { return t ? t.charAt(0).toUpperCase() + t.slice(1) : t; }

  function alertBox(msg) {
    var err = $('#error'); err.hidden = false; err.textContent = capital(msg); show('play');
  }

  // Acciones
  $('#play').addEventListener('click', function () {
    if (!state) return;
    var p = state.cuenta ? api('jugar', {}) : api('sesion', {});
    p.catch(function (e) { alertBox(e.message); });
  });
  $('#cancel-login').addEventListener('click', function () { api('sesion/cancelar', {}); });
  $('#logout').addEventListener('click', function () { api('sesion/cerrar', {}).catch(function (e) { alertBox(e.message); }); });
  document.querySelectorAll('[data-folder]').forEach(function (b) {
    b.addEventListener('click', function () { api('abrir', { carpeta: b.dataset.folder }); });
  });

  function ram() {
    var v = Number($('#ram').value);
    $('#ramval').textContent = v + ' GB';
    $('#ramnote').textContent = v < 4 ? 'Con menos de 4 GB el modpack puede ir con tirones o cerrarse por falta de memoria.'
      : v > 10 ? 'Más de 10 GB no suele mejorar el juego y puede alargar las pausas de limpieza de memoria.'
      : 'Buen valor para este modpack.';
  }
  $('#ram').addEventListener('input', ram);
  $('#settings-form').addEventListener('submit', function (e) {
    e.preventDefault();
    var saved = $('#saved');
    saved.textContent = 'Guardando…';
    api('ajustes', { servidor: $('#server').value, memoria_mb: Number($('#ram').value) * 1024 })
      .then(function () { saved.textContent = 'Guardado'; setTimeout(function () { saved.textContent = ''; }, 2000); })
      .catch(function (err) { saved.textContent = capital(err.message); });
  });

  // Eventos del launcher
  function connect() {
    var es = new EventSource('/api/eventos?t=' + encodeURIComponent(token));
    es.onmessage = function (ev) {
      var m = JSON.parse(ev.data);
      if (m.tipo === 'estado') {
        if (m.datos.actividad !== 'preparando') { stage = ''; progress = null; }
        state = m.datos;
      } else if (m.tipo === 'paso') {
        stage = m.datos.texto; progress = null;
      } else if (m.tipo === 'progreso') {
        progress = m.datos;
      } else if (m.tipo === 'servidor' && state) {
        state.servidor = m.datos;
      }
      render();
    };
    es.onerror = function () { es.close(); setTimeout(connect, 1500); };
  }
  connect();
})();
