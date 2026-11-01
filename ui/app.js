'use strict';
(function () {
  var token = new URLSearchParams(location.search).get('t') || '';
  var $ = function (s) { return document.querySelector(s); };
  var state = null, stage = '', progress = null, download = null, seenSteps = {};

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

  var tabs = document.querySelectorAll('nav button');
  function show(v) {
    tabs.forEach(function (t) { t.setAttribute('aria-selected', t.dataset.view === v ? 'true' : 'false'); });
    ['play', 'updates', 'mods', 'settings'].forEach(function (id) { $('#view-' + id).hidden = id !== v; });
  }
  tabs.forEach(function (t) { t.addEventListener('click', function () { show(t.dataset.view); }); });

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }
  function capital(t) { return t ? t.charAt(0).toUpperCase() + t.slice(1) : t; }
  function size(b) {
    if (!b) return '0 MB';
    if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' GB';
    if (b >= 1048576) return (b / 1048576).toFixed(1) + ' MB';
    return Math.max(1, Math.round(b / 1024)) + ' KB';
  }
  function isWorking(b) { return b === 'preparando' || b === 'actualizando-launcher'; }

  function renderPlay(s) {
    var u = s.actualizacion, busy = s.actividad, title = $('#play-title'), lead = $('#play-lead'), btn = $('#play');
    var needLogin = s.ajustes.modo === 'microsoft' && !s.cuenta;
    btn.disabled = false;
    $('#cancel-login').hidden = busy !== 'sesion';

    if (!s.configurado) {
      title.textContent = 'Falta configurar la publicación';
      lead.textContent = 'Este launcher se compiló sin dirección de GitHub o sin clave pública, así que no puede descargar el modpack. Quien lo distribuye debe seguir docs/ADMINISTRAR_GITHUB.md.';
      btn.textContent = 'Jugar'; btn.disabled = true;
    } else if (busy === 'sesion') {
      title.textContent = 'Inicia sesión en tu navegador';
      lead.textContent = 'Se abrió la página de Microsoft. Vuelve aquí al terminar.';
      btn.textContent = 'Esperando a Microsoft'; btn.disabled = true;
    } else if (busy === 'actualizando-launcher') {
      title.textContent = 'Actualizando el launcher';
      lead.textContent = 'Al terminar la descarga se cerrará y volverá a abrirse solo.';
      btn.textContent = 'Actualizando'; btn.disabled = true;
    } else if (busy === 'preparando') {
      title.textContent = s.instalado ? 'Actualizando' : 'Instalando ' + s.modpack.nombre;
      lead.textContent = s.instalado ? 'Solo se descarga lo que cambió.' : 'La primera vez se descargan Minecraft, Java, Forge y los mods. Si se corta, continúa donde iba.';
      btn.textContent = 'Preparando'; btn.disabled = true;
    } else if (busy === 'jugando') {
      title.textContent = 'Minecraft está abierto';
      lead.textContent = 'Puedes dejar el launcher en segundo plano.';
      btn.textContent = 'Jugando'; btn.disabled = true;
    } else if (busy === 'comprobando') {
      title.textContent = 'Revisando archivos';
      lead.textContent = 'Comparando tu instalación con la versión publicada.';
      btn.textContent = 'Revisando'; btn.disabled = true;
    } else if (!s.modpack.version) {
      title.textContent = 'Sin conexión con GitHub';
      lead.textContent = 'Hace falta conexión para la primera instalación. Comprueba internet y reintenta.';
      btn.textContent = 'Reintentar';
    } else if (needLogin) {
      title.textContent = 'Inicia sesión';
      lead.textContent = 'Elegiste abrir el juego directamente. Usa la cuenta de Microsoft con la que compraste Minecraft: Java Edition.';
      btn.textContent = 'Iniciar sesión';
    } else if (!s.instalado) {
      title.textContent = 'Listo para instalar';
      lead.textContent = s.modpack.nombre + ' ' + s.modpack.version + ': ' + size(u.bytes) + ' de archivos del modpack, más Minecraft, Java y Forge.';
      btn.textContent = 'Instalar';
    } else if (u.pendiente) {
      title.textContent = 'Hay cambios por aplicar';
      lead.textContent = (u.anadir || []).length + (u.actualizar || []).length + (u.eliminar || []).length + ' archivos cambian al actualizar a ' + u.disponible + '.';
      btn.textContent = 'Actualizar y jugar';
    } else {
      title.textContent = 'Todo al día';
      lead.textContent = s.sin_conexion ? 'Sin conexión: juegas con la última versión instalada.' : modeHint(s);
      btn.textContent = 'Jugar';
    }

    $('#v-pack').textContent = u.instalada || 'Ninguna';
    $('#v-mc').textContent = s.modpack.minecraft || '–';
    $('#v-forge').textContent = s.modpack.forge || '–';
    $('#v-umbral').textContent = s.modpack.umbral || '–';

    var working = isWorking(busy);
    $('#install').hidden = !working;
    if (working) renderProgress(); else window.setRune(s.instalado && !u.pendiente ? 1 : 0);

    var notice = $('#notice'); notice.hidden = !s.aviso; notice.textContent = s.aviso || '';
    $('#error').hidden = !s.error; $('#error-text').textContent = capital(s.error || '');
    $('#launcher-new').hidden = !s.launcher_nuevo || !!busy;
    $('#launcher-new-text').textContent = 'Umbral Launcher ' + s.launcher_nuevo + ' está disponible. Tienes la ' + s.version_launcher + '.';

    var news = $('#news'); news.textContent = '';
    (s.modpack.novedades || []).forEach(function (n) { news.appendChild(el('li', '', n)); });
    $('#news-box').hidden = !(s.modpack.novedades || []).length || working;

    var addr = s.ajustes.servidor, st = s.servidor;
    $('#server-box').hidden = !addr || working;
    if (addr) {
      $('#srv-body').textContent = !st ? addr + ': consultando' : st.en_linea
        ? addr + ': en línea, ' + st.jugadores + ' de ' + st.maximo + ' jugadores, ' + st.latencia_ms + ' ms'
        : addr + ': sin respuesta';
    }
  }

  function modeHint(s) {
    if (s.ajustes.modo === 'externo') return 'Jugar comprueba la instancia y te dice dónde está para abrirla con tu launcher.';
    if (s.ajustes.modo === 'microsoft') return 'Jugar abre Minecraft directamente.';
    return 'Jugar abre el launcher oficial con el perfil de ' + s.modpack.nombre + '. Tu cuenta la gestiona Minecraft.';
  }

  function renderProgress() {
    var bar = $('#bar'), wrap = $('#bar-wrap'), st = $('#status'), file = $('#status-file');
    document.querySelectorAll('#steps li').forEach(function (li) {
      var key = li.dataset.step, now = stage.indexOf(key) >= 0;
      if (now) seenSteps[key] = true;
      li.className = now ? 'now' : (seenSteps[key] ? 'done' : '');
    });
    var pct = null;
    if (download && download.bytes_total > 0) {
      pct = Math.min(100, Math.round(download.bytes / download.bytes_total * 100));
      st.textContent = size(download.bytes) + ' de ' + size(download.bytes_total) + (download.velocidad ? ', ' + size(download.velocidad) + '/s' : '') + (download.total > 1 ? ', archivo ' + Math.min(download.hecho + 1, download.total) + ' de ' + download.total : '');
      file.textContent = download.archivo || '';
    } else if (progress && progress.total > 0) {
      pct = Math.round(progress.hecho / progress.total * 100);
      st.textContent = progress.etiqueta + ': ' + progress.hecho + ' de ' + progress.total + ' archivos';
      file.textContent = '';
    } else {
      st.textContent = stage || 'Preparando';
      file.textContent = '';
    }
    wrap.classList.toggle('busy', pct === null);
    bar.style.width = pct === null ? '' : pct + '%';
    if (pct !== null) wrap.setAttribute('aria-valuenow', pct); else wrap.removeAttribute('aria-valuenow');
    window.setRune(pct === null ? .15 : pct / 100);
  }

  function renderUpdates(s) {
    var u = s.actualizacion, busy = !!s.actividad;
    $('#up-dot').hidden = !u.pendiente && !s.launcher_nuevo;
    $('#up-current').textContent = u.instalada || 'Ninguna';
    $('#up-new').textContent = u.disponible || '–';
    $('#up-bytes').textContent = u.pendiente ? size(u.bytes) : '0 MB';
    $('#up-lead').textContent = s.sin_conexion ? 'Sin conexión: esto compara con la última versión que se pudo verificar.'
      : u.pendiente ? 'Estos son los cambios para dejar tu instalación igual que la versión publicada.'
      : u.disponible ? 'Tu instalación coincide archivo por archivo con la versión publicada.' : 'Aún no se ha podido consultar la versión publicada.';
    var box = $('#up-changes'); box.textContent = '';
    [['Se añaden', u.anadir], ['Se reemplazan', u.actualizar], ['Se eliminan', u.eliminar]].forEach(function (g) {
      if (!g[1] || !g[1].length) return;
      box.appendChild(el('h2', '', g[0] + ' (' + g[1].length + ')'));
      var ul = el('ul'); g[1].forEach(function (p) { ul.appendChild(el('li', '', p)); });
      box.appendChild(ul);
    });
    $('#up-apply').disabled = busy || !u.pendiente;
    $('#up-check').disabled = busy;
  }

  function renderMods(s) {
    var tb = $('#extras'); tb.textContent = '';
    $('#extras-box').hidden = !(s.extras || []).length;
    (s.extras || []).forEach(function (m) {
      var row = el('label', 'slot'), info = el('span');
      info.appendChild(el('b', '', m.nombre));
      if (m.descripcion) info.appendChild(el('small', '', m.descripcion));
      var cb = el('input'); cb.type = 'checkbox'; cb.checked = m.activo; cb.disabled = !!s.actividad;
      cb.addEventListener('change', function () {
        api('extra', { archivo: m.archivo, activo: cb.checked }).catch(function (e) { cb.checked = !cb.checked; alertBox(e.message); });
      });
      row.appendChild(info); row.appendChild(cb); tb.appendChild(row);
    });
    var base = $('#base'); base.textContent = '';
    (s.mods || []).forEach(function (m) {
      var row = el('div', 'slot'), info = el('span');
      info.appendChild(el('b', '', m.nombre));
      if (m.descripcion) info.appendChild(el('small', '', m.descripcion));
      row.appendChild(info); base.appendChild(row);
    });
    if (!(s.mods || []).length) base.appendChild(el('small', '', 'La lista aparece al consultar la versión publicada.'));
  }

  function setUnlessFocused(sel, v) { var e = $(sel); if (document.activeElement !== e) e.value = v || ''; }

  function renderSettings(s) {
    var a = s.ajustes;
    $('#mode-ms').disabled = !s.login_configurado;
    $('#mode-ms').hidden = !s.login_configurado;
    setUnlessFocused('#mode', a.modo);
    setUnlessFocused('#server', a.servidor);
    setUnlessFocused('#mcdir', a.carpeta_minecraft);
    setUnlessFocused('#java', a.java_ruta);
    setUnlessFocused('#javaargs', a.java_argumentos);
    setUnlessFocused('#datadir', s.carpeta);
    if (document.activeElement !== $('#ram')) { $('#ram').value = Math.round(a.memoria_mb / 1024); ram(); }
    modeNote();
    $('#account-box').hidden = a.modo !== 'microsoft';
    $('#acc-detail').textContent = s.cuenta ? 'Sesión de ' + s.cuenta.nombre + '. Dura hasta cerrar el launcher: no se guarda en el equipo.' : 'Sin sesión.';
    $('#logout').hidden = !s.cuenta;
    $('#foot').textContent = 'Umbral Launcher ' + s.version_launcher;
  }

  function modeNote() {
    var v = $('#mode').value;
    $('#mode-note').textContent = v === 'oficial' ? 'Crea un perfil en el launcher oficial; ahí eliges cuenta y pulsas Jugar.'
      : v === 'externo' ? 'Umbral solo instala y actualiza. Abres la instancia con el launcher que prefieras.'
      : 'Inicias sesión en la página de Microsoft. La sesión no se guarda al cerrar.';
  }
  $('#mode').addEventListener('change', modeNote);

  function render() {
    if (!state) return;
    renderPlay(state); renderUpdates(state); renderMods(state); renderSettings(state);
    $('#tagline').textContent = state.modpack.version ? 'Versión publicada ' + state.modpack.version : '';
  }

  function alertBox(msg) { $('#error').hidden = false; $('#error-text').textContent = capital(msg); show('play'); }
  function run(path) { return api(path, {}).catch(function (e) { alertBox(e.message); }); }

  $('#play').addEventListener('click', function () {
    if (!state) return;
    seenSteps = {};
    if (!state.modpack.version) return run('comprobar');
    if (state.ajustes.modo === 'microsoft' && !state.cuenta) return run('sesion');
    if (!state.instalado) return run('instalar');
    run('jugar');
  });
  $('#retry').addEventListener('click', function () { seenSteps = {}; run(state && state.modpack.version ? 'instalar' : 'comprobar'); });
  $('#cancel').addEventListener('click', function () { run('cancelar'); $('#status').textContent = 'Cancelando'; });
  $('#cancel-login').addEventListener('click', function () { run('sesion/cancelar'); });
  $('#logout').addEventListener('click', function () { run('sesion/cerrar'); });
  $('#launcher-update').addEventListener('click', function () { run('launcher/actualizar'); });
  $('#up-apply').addEventListener('click', function () { seenSteps = {}; show('play'); run('instalar'); });
  $('#up-check').addEventListener('click', function () { run('comprobar'); });
  document.querySelectorAll('[data-folder]').forEach(function (b) {
    b.addEventListener('click', function () { api('abrir', { carpeta: b.dataset.folder }); });
  });
  $('#diag-btn').addEventListener('click', function () {
    api('diagnostico').then(function (d) {
      $('#diag').hidden = false; $('#diag-copy').hidden = false;
      $('#diag').textContent = JSON.stringify(d, null, 2);
    });
  });
  $('#diag-copy').addEventListener('click', function () { navigator.clipboard.writeText($('#diag').textContent); });

  function ram() {
    var v = Number($('#ram').value);
    $('#ramval').textContent = v + ' GB';
    $('#ramnote').textContent = v < 4 ? 'Con menos de 4 GB el modpack puede ir con tirones o cerrarse por falta de memoria.'
      : v > 10 ? 'Más de 10 GB no suele mejorar el juego y puede alargar las pausas.' : 'Buen valor para este modpack.';
  }
  $('#ram').addEventListener('input', ram);
  $('#settings-form').addEventListener('submit', function (e) {
    e.preventDefault();
    var saved = $('#saved'); saved.textContent = 'Guardando';
    api('ajustes', {
      servidor: $('#server').value, memoria_mb: Number($('#ram').value) * 1024, modo: $('#mode').value,
      carpeta_minecraft: $('#mcdir').value, java_ruta: $('#java').value, java_argumentos: $('#javaargs').value
    }).then(function () { saved.textContent = 'Ajustes guardados'; setTimeout(function () { saved.textContent = ''; }, 2500); })
      .catch(function (err) { saved.textContent = capital(err.message); });
  });
  $('#location-form').addEventListener('submit', function (e) {
    e.preventDefault();
    api('ubicacion', { carpeta: $('#datadir').value }).catch(function (err) { alertBox(err.message); });
  });

  function connect() {
    var es = new EventSource('/api/eventos?t=' + encodeURIComponent(token));
    es.onmessage = function (ev) {
      var m = JSON.parse(ev.data);
      if (m.tipo === 'estado') {
        if (!isWorking(m.datos.actividad)) { stage = ''; progress = null; download = null; }
        state = m.datos;
      } else if (m.tipo === 'paso') {
        stage = m.datos.texto; progress = null; download = null;
      } else if (m.tipo === 'progreso') {
        progress = m.datos; download = null;
      } else if (m.tipo === 'descarga') {
        download = m.datos;
      } else if (m.tipo === 'servidor' && state) {
        state.servidor = m.datos;
      }
      render();
    };
    es.onerror = function () { es.close(); setTimeout(connect, 1500); };
  }
  connect();
})();
