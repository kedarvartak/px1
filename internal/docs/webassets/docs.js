// Theme, mobile navigation and search for the documentation site. The site
// works without this file: every page is plain HTML with a full sidebar.
(function () {
  var root = document.documentElement;

  function stored() {
    try { return localStorage.getItem('px1-docs-theme'); } catch (e) { return null; }
  }
  var theme = stored();
  if (theme === 'light' || theme === 'dark') root.dataset.theme = theme;

  var themeBtn = document.getElementById('theme');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      var next = root.dataset.theme === 'light' ? 'dark' : 'light';
      root.dataset.theme = next;
      try { localStorage.setItem('px1-docs-theme', next); } catch (e) { /* private window */ }
    });
  }

  var menu = document.getElementById('menu');
  if (menu) {
    menu.addEventListener('click', function () {
      var open = document.body.classList.toggle('nav-open');
      menu.setAttribute('aria-expanded', String(open));
    });
  }

  var input = document.getElementById('q');
  var list = document.getElementById('results');
  if (!input || !list) return;
  var index = null;
  var loading = null;
  var active = -1;

  function load() {
    if (!loading) {
      loading = fetch('search.json')
        .then(function (r) { return r.json(); })
        .then(function (data) { index = data; })
        .catch(function () { index = []; });
    }
    return loading;
  }

  // Every word of the query must appear; a title match outranks a heading
  // match, which outranks body text.
  function search(query) {
    var words = query.toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return [];
    var hits = [];
    index.forEach(function (page) {
      var title = page.title.toLowerCase();
      var headings = page.headings.join(' \n ').toLowerCase();
      var text = page.text.toLowerCase();
      var score = 0;
      for (var i = 0; i < words.length; i++) {
        var w = words[i];
        if (title.indexOf(w) >= 0) score += 10;
        else if (headings.indexOf(w) >= 0) score += 4;
        else if (text.indexOf(w) >= 0) score += 1;
        else return;
      }
      var at = text.indexOf(words[0]);
      var from = Math.max(0, at - 40);
      var snippet = at >= 0 ? (from > 0 ? '…' : '') + page.text.slice(from, from + 120) : page.section;
      hits.push({ page: page, score: score, snippet: snippet });
    });
    hits.sort(function (a, b) { return b.score - a.score || a.page.title.localeCompare(b.page.title); });
    return hits.slice(0, 8);
  }

  function render(hits, query) {
    list.textContent = '';
    active = -1;
    if (!query.trim()) { list.hidden = true; return; }
    if (!hits.length) {
      var none = document.createElement('li');
      none.className = 'none';
      none.textContent = 'No page matches “' + query + '”.';
      list.appendChild(none);
    }
    hits.forEach(function (hit) {
      var li = document.createElement('li');
      var a = document.createElement('a');
      a.href = hit.page.url;
      a.textContent = hit.page.title;
      var small = document.createElement('small');
      small.textContent = hit.snippet;
      a.appendChild(small);
      li.appendChild(a);
      list.appendChild(li);
    });
    list.hidden = false;
  }

  function move(step) {
    var links = list.querySelectorAll('a');
    if (!links.length) return;
    if (active >= 0) links[active].classList.remove('on');
    active = (active + step + links.length) % links.length;
    links[active].classList.add('on');
    links[active].scrollIntoView({ block: 'nearest' });
  }

  input.addEventListener('focus', load);
  input.addEventListener('input', function () {
    var query = input.value;
    load().then(function () {
      if (input.value === query) render(search(query), query);
    });
  });
  input.addEventListener('keydown', function (e) {
    if (e.key === 'ArrowDown') { e.preventDefault(); move(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1); }
    else if (e.key === 'Enter') {
      var links = list.querySelectorAll('a');
      var target = links[active >= 0 ? active : 0];
      if (target) { e.preventDefault(); location.href = target.href; }
    } else if (e.key === 'Escape') { list.hidden = true; input.blur(); }
  });
  document.addEventListener('click', function (e) {
    if (!list.contains(e.target) && e.target !== input) list.hidden = true;
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === '/' && document.activeElement !== input) { e.preventDefault(); input.focus(); }
  });
})();
