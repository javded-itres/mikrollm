(function () {
  function escHtml(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c];
    });
  }
  function csrfToken() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? (m.getAttribute("content") || "") : "";
  }
  document.querySelectorAll("form[method='post'], form[method='POST']").forEach(function (f) {
    if (f.querySelector("input[name=csrf]")) return;
    var i = document.createElement("input");
    i.type = "hidden";
    i.name = "csrf";
    i.value = csrfToken();
    f.appendChild(i);
  });

  document.querySelectorAll("[data-kind-form]").forEach(function (root) {
    var form = root.querySelector("form") || root;
    var sel = form.querySelector("select[name=kind]");
    if (!sel) return;
    function sync() {
      var k = sel.value;
      form.querySelectorAll("[data-for-kind]").forEach(function (el) {
        var kinds = (el.getAttribute("data-for-kind") || "").split(/\s+/);
        el.hidden = kinds.indexOf(k) === -1;
      });
    }
    sel.addEventListener("change", sync);
    sync();
  });

  document.querySelectorAll("[data-toggle]").forEach(function (el) {
    el.addEventListener("change", function () {
      var root = document.querySelector(el.getAttribute("data-toggle"));
      if (!root) return;
      root.querySelectorAll('input[type="checkbox"][name="model"]:not(:disabled)').forEach(function (cb) {
        var row = cb.closest(".model-tr, .pick");
        if (row && row.classList.contains("is-hidden")) return;
        cb.checked = el.checked;
      });
    });
  });

  document.querySelectorAll("[data-copy]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var node = document.querySelector(btn.getAttribute("data-copy"));
      if (!node) return;
      var text = node.textContent.trim();
      function ok() {
        var old = btn.textContent;
        btn.textContent = "Скопировано";
        setTimeout(function () { btn.textContent = old; }, 1500);
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(ok);
      } else {
        var ta = document.createElement("textarea");
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        ta.remove();
        ok();
      }
    });
  });

  var up = document.getElementById("upstream");
  if (up) {
    up.addEventListener("change", function () {
      var opt = up.options[up.selectedIndex];
      var alias = document.getElementById("alias");
      if (alias && !alias.value) alias.placeholder = opt.value;
      var ids = (opt.getAttribute("data-backends") || "").split(",");
      document.querySelectorAll('#backend-checks input[name="backend_id"]').forEach(function (cb) {
        cb.checked = ids.indexOf(cb.value) !== -1;
      });
    });
  }

  var all = document.getElementById("all-models");
  var box = document.getElementById("key-models");
  if (all && box) {
    function sync() {
      box.classList.toggle("is-disabled", all.checked);
      var picked = document.getElementById("key-picked");
      var filters = document.querySelector(".key-filters");
      if (picked) picked.classList.toggle("is-disabled", all.checked);
      if (filters) filters.classList.toggle("is-disabled", all.checked);
      box.querySelectorAll("input").forEach(function (cb) {
        cb.disabled = all.checked;
      });
    }
    all.addEventListener("change", sync);
    sync();
  }

  function matchBand(band, prompt, want) {
    if (!want) return true;
    var p = parseFloat(prompt || "0") || 0;
    if (want === "none") return band === "none";
    if (want === "free") return band === "free";
    if (want === "paid") return band !== "none" && band !== "free";
    if (want === "lt0.5") return band !== "none" && p < 0.5;
    if (want === "lt2") return band !== "none" && p < 2;
    if (want === "lt10") return band !== "none" && p < 10;
    if (want === "more") return band !== "none" && p >= 10;
    return true;
  }

  var catalogPage = 1;
  function paginateCatalog(reset) {
    var sizeEl = document.getElementById("catalog-page-size");
    var pager = document.getElementById("catalog-pager");
    if (!sizeEl) return;
    if (reset) catalogPage = 1;
    var rows = Array.prototype.filter.call(document.querySelectorAll(".model-tr"), function (el) {
      return !el.classList.contains("is-hidden");
    });
    var n = rows.length;
    var sz = parseInt(sizeEl.value, 10);
    if (isNaN(sz)) sz = 10;
    rows.forEach(function (el) { el.classList.remove("is-paged-out"); });
    if (!pager) return;
    pager.innerHTML = "";
    if (!sz || sz <= 0) {
      return;
    }
    var pages = Math.max(1, Math.ceil(n / sz) || 1);
    if (catalogPage > pages) catalogPage = pages;
    if (catalogPage < 1) catalogPage = 1;
    var from = (catalogPage - 1) * sz;
    var to = Math.min(n, from + sz);
    rows.forEach(function (el, i) {
      el.classList.toggle("is-paged-out", i < from || i >= to);
    });
    function btn(label, toPage, disabled) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "ghost btn-sm";
      b.textContent = label;
      b.disabled = !!disabled;
      b.addEventListener("click", function () {
        catalogPage = toPage;
        paginateCatalog(false);
      });
      pager.appendChild(b);
    }
    btn("←", catalogPage - 1, catalogPage <= 1);
    var info = document.createElement("span");
    info.className = "muted";
    info.textContent = (n ? (from + 1) : 0) + "–" + to + " из " + n;
    pager.appendChild(info);
    btn("→", catalogPage + 1, catalogPage >= pages);
  }

  function bindModelFilters(nameId, provId, priceId, rowSel, countId) {
    var nameEl = document.getElementById(nameId);
    var provEl = document.getElementById(provId);
    var priceEl = document.getElementById(priceId);
    var mediaEl = document.getElementById("model-media");
    var serverEl = document.getElementById("model-server");
    if (!nameEl && !provEl && !priceEl && !serverEl) return;
    var onlyEl = document.getElementById(rowSel.indexOf("#key-models") >= 0 ? "key-only-picked" : "");
    function apply() {
      var q = (nameEl && nameEl.value ? nameEl.value : "").trim().toLowerCase();
      var prov = provEl ? provEl.value : "";
      var price = priceEl ? priceEl.value : "";
      var media = mediaEl ? mediaEl.value : "";
      var server = serverEl ? serverEl.value : "";
      var only = onlyEl && onlyEl.checked;
      var vis = 0, total = 0;
      document.querySelectorAll(rowSel).forEach(function (el) {
        total++;
        var name = (el.getAttribute("data-name") || "").toLowerCase();
        var title = (el.getAttribute("data-title") || "").toLowerCase();
        var provider = el.getAttribute("data-provider") || "";
        var rowMedia = (el.getAttribute("data-media") || "").toLowerCase();
        var servers = (el.getAttribute("data-servers") || "").split(",");
        var isHub = el.getAttribute("data-hub") === "1";
        var cb = el.querySelector("input[name=model]");
        var ok = true;
        if (q && name.indexOf(q) < 0 && title.indexOf(q) < 0 && provider.toLowerCase().indexOf(q) < 0) ok = false;
        if (prov && provider !== prov) ok = false;
        if (server === "__hub__" && !isHub) ok = false;
        if (server && server !== "__hub__") {
          var hit = false;
          for (var i = 0; i < servers.length; i++) {
            if (servers[i] === server) { hit = true; break; }
          }
          if (!hit) ok = false;
        }
        if (!matchBand(el.getAttribute("data-band") || "none", el.getAttribute("data-prompt"), price)) ok = false;
        if (media === "image" && rowMedia.indexOf("image") < 0) ok = false;
        if (media === "video" && rowMedia.indexOf("video") < 0) ok = false;
        if (media === "chat" && rowMedia) ok = false;
        if (only && (!cb || !cb.checked)) ok = false;
        el.classList.toggle("is-hidden", !ok);
        if (ok) vis++;
      });
      if (rowSel === ".model-tr") paginateCatalog(true);
      document.querySelectorAll("#key-models .pick-group").forEach(function (g) {
        var n = g.nextElementSibling, any = false;
        while (n && !n.classList.contains("pick-group")) {
          if (n.classList.contains("pick") && !n.classList.contains("is-hidden")) any = true;
          n = n.nextElementSibling;
        }
        g.classList.toggle("is-hidden", !any);
      });
      var cnt = document.getElementById(countId);
      if (cnt && total) cnt.textContent = vis + " из " + total;
    }
    [nameEl, provEl, priceEl, mediaEl, serverEl, onlyEl].forEach(function (el) {
      if (!el) return;
      el.addEventListener("input", apply);
      el.addEventListener("change", apply);
    });
    apply();
    return apply;
  }
  bindModelFilters("model-filter", "model-provider", "model-price", ".model-tr", "catalog-count");
  (function aliasHubFilter() {
    var sel = document.getElementById("alias-hub-filter");
    if (!sel) return;
    function apply() {
      var v = sel.value;
      var vis = 0, total = 0;
      document.querySelectorAll(".alias-tr").forEach(function (el) {
        total++;
        var h = el.getAttribute("data-hub") || "";
        var ok = !v || h === v;
        el.classList.toggle("is-hidden", !ok);
        if (ok) vis++;
      });
      var cnt = document.getElementById("alias-hub-count");
      if (cnt) cnt.textContent = vis + " из " + total;
    }
    sel.addEventListener("change", apply);
    apply();
  })();
  (function () {
    var sizeEl = document.getElementById("catalog-page-size");
    if (!sizeEl) return;
    try {
      var s = localStorage.getItem("ml-catalog-page-size");
      if (s != null && s !== "") sizeEl.value = s;
    } catch (e) {}
    sizeEl.addEventListener("change", function () {
      try { localStorage.setItem("ml-catalog-page-size", sizeEl.value); } catch (e) {}
      paginateCatalog(true);
    });
    paginateCatalog(false);
  })();
  var applyKeyFilters = bindModelFilters("key-model-filter", "key-provider", "key-price", "#key-models .pick", "key-count");

  (function keyPicker() {
    var list = document.getElementById("key-models");
    var chips = document.getElementById("key-picked-chips");
    var nEl = document.getElementById("key-picked-n");
    var empty = document.getElementById("key-picked-empty");
    if (!list || !chips) return;
    function syncPicked() {
      var selected = [];
      list.querySelectorAll("input[name=model]:checked").forEach(function (cb) {
        selected.push(cb.value);
      });
      if (nEl) nEl.textContent = String(selected.length);
      if (empty) empty.hidden = selected.length > 0;
      chips.textContent = "";
      selected.forEach(function (name) {
        var btn = document.createElement("button");
        btn.type = "button";
        btn.className = "chip";
        btn.title = "убрать " + name;
        btn.textContent = name + " ×";
        btn.addEventListener("click", function () {
          list.querySelectorAll("input[name=model]").forEach(function (cb) {
            if (cb.value === name) cb.checked = false;
          });
          syncPicked();
          if (applyKeyFilters) applyKeyFilters();
        });
        chips.appendChild(btn);
      });
    }
    list.addEventListener("change", function () {
      syncPicked();
      if (applyKeyFilters) applyKeyFilters();
    });
    var clearBtn = document.getElementById("key-picked-clear");
    if (clearBtn) {
      clearBtn.addEventListener("click", function () {
        list.querySelectorAll("input[name=model]:checked").forEach(function (cb) { cb.checked = false; });
        syncPicked();
        if (applyKeyFilters) applyKeyFilters();
      });
    }
    document.querySelectorAll("[data-group-check]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        var g = btn.getAttribute("data-group-check");
        list.querySelectorAll(".pick[data-group=\"" + g + "\"]:not(.is-hidden) input[name=model]:not(:disabled)").forEach(function (cb) {
          cb.checked = true;
        });
        syncPicked();
        if (applyKeyFilters) applyKeyFilters();
      });
    });
    syncPicked();
  })();

  (function logFilters() {
    var bar = document.getElementById("log-filters");
    if (!bar) return;
    var qEl = document.getElementById("log-q");
    var stEl = document.getElementById("log-status");
    var modelEl = document.getElementById("log-model");
    var backEl = document.getElementById("log-backend");
    var keyEl = document.getElementById("log-key");
    var msEl = document.getElementById("log-ms");
    var cnt = document.getElementById("log-count");
    var empty = document.getElementById("log-empty");
    var params = new URLSearchParams(location.search);
    function setSel(el, v) {
      if (!el || !v) return;
      for (var i = 0; i < el.options.length; i++) {
        if (el.options[i].value === v) { el.selectedIndex = i; return; }
      }
    }
    if (qEl && params.get("q")) qEl.value = params.get("q");
    setSel(stEl, params.get("status"));
    setSel(modelEl, params.get("model"));
    setSel(backEl, params.get("backend"));
    setSel(keyEl, params.get("key"));
    setSel(msEl, params.get("ms"));
    function apply() {
      var q = (qEl && qEl.value ? qEl.value : "").trim().toLowerCase();
      var st = stEl ? stEl.value : "";
      var model = modelEl ? modelEl.value : "";
      var backend = backEl ? backEl.value : "";
      var key = keyEl ? keyEl.value : "";
      var ms = msEl ? msEl.value : "";
      var vis = 0, total = 0;
      document.querySelectorAll(".log-tr").forEach(function (tr) {
        total++;
        var ok = true;
        var hay = (tr.getAttribute("data-q") || "").toLowerCase();
        if (q && hay.indexOf(q) < 0) ok = false;
        if (st && tr.getAttribute("data-kind") !== st) ok = false;
        if (model && tr.getAttribute("data-model") !== model) ok = false;
        if (backend && tr.getAttribute("data-backend") !== backend) ok = false;
        if (key && tr.getAttribute("data-key") !== key) ok = false;
        var n = parseInt(tr.getAttribute("data-ms") || "0", 10) || 0;
        if (ms === "fast" && n >= 100) ok = false;
        if (ms === "mid" && (n < 100 || n >= 1000)) ok = false;
        if (ms === "slow" && n < 1000) ok = false;
        tr.classList.toggle("is-hidden", !ok);
        if (ok) vis++;
      });
      if (cnt) cnt.textContent = vis + " из " + total;
      if (empty) empty.hidden = vis > 0 || total === 0;
      var usp = new URLSearchParams();
      if (q) usp.set("q", qEl.value.trim());
      if (st) usp.set("status", st);
      if (model) usp.set("model", model);
      if (backend) usp.set("backend", backend);
      if (key) usp.set("key", key);
      if (ms) usp.set("ms", ms);
      var next = location.pathname + (usp.toString() ? "?" + usp.toString() : "");
      if (next !== location.pathname + location.search) history.replaceState(null, "", next);
    }
    [qEl, stEl, modelEl, backEl, keyEl, msEl].forEach(function (el) {
      if (!el) return;
      el.addEventListener("input", apply);
      el.addEventListener("change", apply);
    });
    var clear = document.getElementById("log-clear");
    if (clear) {
      clear.addEventListener("click", function () {
        if (qEl) qEl.value = "";
        [stEl, modelEl, backEl, keyEl, msEl].forEach(function (el) { if (el) el.selectedIndex = 0; });
        apply();
      });
    }
    apply();
  })();

  var keyListFilter = document.getElementById("key-list-filter");
  if (keyListFilter) {
    keyListFilter.addEventListener("input", function () {
      var q = keyListFilter.value.trim().toLowerCase();
      document.querySelectorAll(".key-tr").forEach(function (tr) {
        var name = (tr.getAttribute("data-name") || "").toLowerCase();
        tr.classList.toggle("is-hidden", q && name.indexOf(q) === -1);
      });
    });
  }

  var kindSel = document.getElementById("backend-kind");
  var urlInp = document.getElementById("backend-url");
  var kindHint = document.getElementById("backend-kind-hint");
  var tokenInp = document.getElementById("backend-token");
  var tokenLabel = document.getElementById("backend-token-label");
  var kindMeta = {
    ollama: {
      url: "http://192.168.88.82:11434",
      hint: "Локальный Ollama: pull, load/unload и удаление файла — из админки.",
      token: false,
      fill: false
    },
    vllm: {
      url: "http://192.168.88.82:8000",
      hint: "vLLM не меняет модель через API. На сервере: vllm serve <HuggingFace-id> --host 0.0.0.0 --port 8000.",
      token: false,
      fill: false
    },
    lmstudio: {
      url: "http://192.168.88.82:1234",
      hint: "LM Studio: включите локальный сервер (Developer). Pull и load/unload — из админки.",
      token: false,
      fill: false
    },
    openrouter: {
      url: "https://openrouter.ai/api/v1",
      hint: "Прямое облако. API-ключ с openrouter.ai/keys. Свой GPU-сервер не нужен.",
      token: true,
      fill: true
    },
    "ollama-cloud": {
      url: "https://ollama.com",
      hint: "Только облачные модели ollama.com, без локального Ollama. Ключ: ollama.com/settings/keys.",
      token: true,
      fill: true
    },
    opencomfy: {
      url: "http://192.168.88.252:8788",
      hint: "ComfyUI через OpenComfy (картинки и видео). URL без /v1. Ключ sk- из keys.yaml OpenComfy.",
      token: true,
      fill: false
    }
  };
  var knownURLs = {
    "http://192.168.88.82:11434": 1,
    "http://192.168.88.82:8000": 1,
    "http://192.168.88.82:1234": 1,
    "https://openrouter.ai/api/v1": 1,
    "https://ollama.com": 1,
    "http://192.168.88.252:8788": 1
  };
  function syncKind() {
    if (!kindSel) return;
    var meta = kindMeta[kindSel.value] || kindMeta.ollama;
    if (urlInp) {
      var cur = (urlInp.value || "").replace(/\/$/, "");
      if (!cur || knownURLs[cur]) {
        if (meta.fill) urlInp.value = meta.url;
        else if (knownURLs[cur]) urlInp.value = "";
      }
      urlInp.placeholder = meta.url;
    }
    if (kindHint) kindHint.textContent = meta.hint;
    if (tokenInp) tokenInp.required = !!meta.token;
    if (tokenLabel) tokenLabel.textContent = meta.token ? "API-ключ (обязательно)" : "Токен (необязательно)";
  }
  if (kindSel) {
    kindSel.addEventListener("change", syncKind);
    syncKind();
  }

  function syncPullPlaceholder() {
    var sel = document.getElementById("pull-backend");
    var name = document.getElementById("pull-name");
    if (!sel || !name) return;
    var opt = sel.options[sel.selectedIndex];
    var kind = opt && opt.getAttribute("data-kind");
    if (kind === "lmstudio") {
      name.placeholder = "ibm/granite-4-micro или huggingface.co/…";
    } else {
      name.placeholder = "llama3.2 или qwen3.8:27b-mlx";
    }
  }
  var pullBackend = document.getElementById("pull-backend");
  if (pullBackend) {
    pullBackend.addEventListener("change", syncPullPlaceholder);
    syncPullPlaceholder();
  }

  document.querySelectorAll("[data-ollama]").forEach(function (btn) {
    btn.addEventListener("click", function (ev) {
      ev.preventDefault();
      var action = btn.getAttribute("data-ollama");
      var id = btn.getAttribute("data-id");
      var name = btn.getAttribute("data-name");
      var from = btn.getAttribute("data-name-from");
      if (from) {
        var sel = document.getElementById(from);
        if (sel) name = (sel.value || "").trim();
      }
      if (!id || !name) {
        alert("Выберите модель");
        return;
      }
      var verb = "Выгрузить «" + name + "» из RAM?";
      if (action === "delete") verb = "Удалить «" + name + "» с диска сервера?";
      if (action === "load") verb = "Загрузить «" + name + "» в RAM? Для большой модели это может занять несколько минут.";
      if (!confirm(verb)) return;
      btn.disabled = true;
      var old = btn.textContent;
      if (action === "load") btn.textContent = "загрузка…";
      var body = new URLSearchParams({ name: name });
      fetch("/admin/ollama/" + id + "/" + action, {
        method: "POST",
        headers: { "Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": csrfToken() },
        body: body,
        credentials: "same-origin"
      }).then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
        .then(function (res) {
          if (!res.ok) throw new Error(res.j.error || "ошибка");
          if (action === "load") {
            pollJobs();
            return;
          }
          location.reload();
        }).catch(function (e) {
          alert(e.message || e);
          btn.disabled = false;
          btn.textContent = old;
        });
    });
  });

  var pullForm = document.getElementById("pull-form");
  if (pullForm) {
    pullForm.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var id = document.getElementById("pull-backend").value;
      var name = document.getElementById("pull-name").value.trim();
      if (!id || !name) return;
      var btn = pullForm.querySelector("button[type=submit]");
      btn.disabled = true;
      fetch("/admin/ollama/" + id + "/pull", {
        method: "POST",
        headers: { "Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded", "X-CSRF-Token": csrfToken() },
        body: new URLSearchParams({ name: name }),
        credentials: "same-origin"
      }).then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
        .then(function (res) {
          if (!res.ok) throw new Error(res.j.error || "ошибка");
          var card = document.getElementById("pull-card");
          if (card) card.open = true;
          pollJobs();
        }).catch(function (e) {
          alert(e.message || e);
          btn.disabled = false;
        });
    });
  }

  var jobTimer = 0;
  var watchedRunning = false;
  var hideBannerTimer = 0;
  var flashEl = document.querySelector(".flash");
  if (flashEl) {
    setTimeout(function () { flashEl.hidden = true; }, 5000);
  }
  function pollJobs() {
    if (jobTimer) {
      clearTimeout(jobTimer);
      jobTimer = 0;
    }
    fetch("/admin/ollama/jobs", { credentials: "same-origin", headers: { Accept: "application/json" } })
      .then(function (r) { return r.json(); })
      .then(renderJobs)
      .catch(function () {
        jobTimer = setTimeout(pollJobs, 2000);
      });
  }

  function hideJobBanner() {
    var banner = document.getElementById("job-banner");
    if (banner) banner.hidden = true;
    hideBannerTimer = 0;
  }

  function renderJobs(data) {
    var jobs = data.jobs || [];
    var banner = document.getElementById("job-banner");
    var running = jobs.filter(function (j) { return j.status === "running"; });
    var latest = running[0];
    if (!latest && watchedRunning) {
      latest = jobs[0];
    }
    if (banner) {
      if (!latest) {
        banner.hidden = true;
      } else {
        banner.hidden = false;
        banner.classList.toggle("is-err", latest.status === "error");
        banner.classList.toggle("is-ok", latest.status === "done");
        var title = latest.kind === "load" ? "Загрузка в RAM" : "Скачивание";
        if (latest.kind === "load" && latest.status === "done") title = "В RAM";
        if (latest.kind === "pull" && latest.status === "done") title = "Скачано";
        var host = (latest.backend || "").replace(/^mac-/, "");
        var pct = latest.kind === "pull" && latest.status === "running"
          ? latest.percent + "%"
          : "";
        var bar = "";
        if (latest.status === "running" && latest.kind === "pull") {
          bar = "<progress max=\"100\" value=\"" + (latest.percent || 0) + "\"></progress>";
        } else if (latest.status === "running") {
          bar = "<progress></progress>";
        }
        banner.innerHTML =
          "<div class=\"job-title\">" + escHtml(title) + " · " + escHtml(host) + " · " + escHtml(latest.model) +
          (pct ? " · " + escHtml(pct) : "") + "</div>" +
          "<div class=\"muted\">" + escHtml(latest.error || latest.message || "") +
          (latest.status === "running" ? " · можно обновить страницу — задача не прервётся" : "") +
          "</div>" + bar;
      }
    }
    var pullJob = running.filter(function (j) { return j.kind === "pull"; })[0]
      || jobs.filter(function (j) { return j.kind === "pull"; })[0];
    var log = document.getElementById("pull-log");
    var bar = document.getElementById("pull-bar");
    var st = document.getElementById("pull-status");
    var pullBtn = pullForm && pullForm.querySelector("button[type=submit]");
    if (pullJob && log && bar && st) {
      var card = document.getElementById("pull-card");
      if (card && pullJob.status === "running") card.open = true;
      log.hidden = false;
      bar.hidden = false;
      log.textContent = pullJob.log || "";
      log.scrollTop = log.scrollHeight;
      bar.value = pullJob.percent || 0;
      st.textContent = pullJob.error || pullJob.message || "";
      if (pullBtn) pullBtn.disabled = pullJob.status === "running";
    } else if (pullBtn) {
      pullBtn.disabled = false;
    }
    document.querySelectorAll("[data-ollama=load]").forEach(function (btn) {
      var id = btn.getAttribute("data-id");
      var name = btn.getAttribute("data-name");
      var match = running.filter(function (j) {
        return String(j.backend_id) === String(id) && j.model === name && j.kind === "load";
      })[0];
      if (match) {
        btn.disabled = true;
        btn.textContent = "загрузка…";
      }
    });
    if (data.running) {
      watchedRunning = true;
      if (hideBannerTimer) {
        clearTimeout(hideBannerTimer);
        hideBannerTimer = 0;
      }
      jobTimer = setTimeout(pollJobs, 800);
    } else if (watchedRunning) {
      watchedRunning = false;
      var failed = jobs.some(function (j) { return j.status === "error"; });
      if (hideBannerTimer) clearTimeout(hideBannerTimer);
      hideBannerTimer = setTimeout(hideJobBanner, 5000);
      if (!failed && !document.getElementById("chat-form")) {
        setTimeout(function () { location.reload(); }, 5000);
      }
    }
  }

  pollJobs();

  (function queueBoard() {
    var board = document.getElementById("q-board");
    if (!board || !board.getAttribute("data-live")) return;
    var box = document.getElementById("q-list");
    if (!box) return;
    function esc(s) {
      return String(s || "").replace(/[&<>"]/g, function (c) {
        return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c];
      });
    }
    function stLabel(s) {
      return ({ waiting: "ждет", running: "идёт", done: "готово", error: "ошибка", dropped: "сброшен", overflow: "запас", canceled: "отмена" })[s] || s;
    }
    function render(data) {
      var qs = (data && data.queues) || [];
      if (!qs.length) {
        box.innerHTML = "<p class=\"muted\">Очередь пуста. Создайте её на вкладке «Очереди».</p>";
        return;
      }
      box.innerHTML = qs.map(function (q) {
        var lanes = (q.steps || []).map(function (s) {
          var full = s.cap && s.busy >= s.cap;
          return "<div class=\"q-lane" + (full ? " is-full" : "") + (s.healthy ? "" : " is-down") + "\">" +
            "<div class=\"q-lane-h\">" + esc(s.alias) + "</div>" +
            "<div class=\"q-lane-p\">" + esc(s.provider || "—") + (s.healthy ? "" : " · нет связи") + "</div>" +
            "<div class=\"q-bar\"><span style=\"width:" + (s.percent || 0) + "%\"></span></div>" +
            "<div class=\"q-lane-n\">" + s.busy + "/" + s.cap + "</div></div>";
        }).join("");
        var jobs = (q.jobs || []).slice(0, 12).map(function (j) {
          var where = j.status === "waiting" ? "в очереди" : esc([j.provider, j.backend, j.model].filter(Boolean).join(" · "));
          return "<li><span class=\"seq\">#" + j.seq + "</span><span class=\"st-" + esc(j.status) + "\">" + stLabel(j.status) + "</span>" +
            "<span>" + where + "</span><span class=\"muted\">" + (j.key_prefix || "") + "</span></li>";
        }).join("");
        return "<article class=\"q-card\"><div class=\"q-title\"><span class=\"host-name\">" + esc(q.name) + "</span> <code>" + esc(q.alias) + "</code>" +
          "<span class=\"pill\">ждет " + q.waiting + "</span><span class=\"pill ram\">идёт " + q.running + "</span></div>" +
          "<div class=\"q-lanes\">" + (lanes || "<p class=\"muted\">нет шагов</p>") + "</div>" +
          (jobs ? "<ul class=\"q-jobs\">" + jobs + "</ul>" : "") + "</article>";
      }).join("");
    }
    function tick() {
      fetch("/admin/queues/live", { credentials: "same-origin", headers: { Accept: "application/json" } })
        .then(function (r) { return r.json(); })
        .then(render)
        .catch(function () {})
        .then(function () { setTimeout(tick, 1000); });
    }
    tick();
  })();

  (function chatPlayground() {
    var form = document.getElementById("chat-form");
    if (!form) return;
    var log = document.getElementById("chat-log");
    var empty = document.getElementById("chat-empty");
    var input = document.getElementById("chat-input");
    var send = document.getElementById("chat-send");
    var stop = document.getElementById("chat-stop");
    var status = document.getElementById("chat-status");
    var modelEl = document.getElementById("chat-model");
    var modeEl = document.getElementById("gen-mode");
    var pathEl = document.getElementById("gen-path");
    var messages = [];
    var ac = null;
    var pinBottom = true;
    var refs = [];
    var MAX_REFS = 6;
    var refInput = document.getElementById("chat-ref-input");
    var refBtn = document.getElementById("chat-ref-btn");
    var refStrip = document.getElementById("chat-refs");
    var paramsCache = {};
    var paramsSchema = null;
    var paramsReq = 0;
    var saved = localStorage.getItem("ml-chat-model");
    if (saved && modelEl) {
      for (var i = 0; i < modelEl.options.length; i++) {
        if (modelEl.options[i].value === saved) { modelEl.selectedIndex = i; break; }
      }
    }
    function genMode() {
      return (modeEl && modeEl.value) || "chat";
    }
    function optionMedia() {
      var o = modelEl && modelEl.selectedOptions && modelEl.selectedOptions[0];
      return o ? (o.getAttribute("data-media") || "") : "";
    }
    function mediaKind(media) {
      var v = media.indexOf("video") >= 0;
      var im = media.indexOf("image") >= 0;
      if (v && !im) return "video";
      if (im && !v) return "image";
      return "chat";
    }
    function syncModeToModel() {
      var want = mediaKind(optionMedia());
      if (modeEl && want !== "chat" && modeEl.value !== want) {
        modeEl.value = want;
      }
      filterModels();
    }
    function filterModels() {
      if (!modelEl) return;
      var mode = genMode();
      var opts = modelEl.querySelectorAll("option");
      var first = null;
      opts.forEach(function (o) {
        if (!o.value) return;
        var media = o.getAttribute("data-media") || "";
        var ok = true;
        if (mode === "image") ok = media.indexOf("image") >= 0;
        if (mode === "video") ok = media.indexOf("video") >= 0;
        o.hidden = !ok;
        o.disabled = !ok;
        if (ok && !first) first = o;
      });
      if (modelEl.selectedOptions[0] && modelEl.selectedOptions[0].hidden && first) {
        first.selected = true;
      }
      if (pathEl && modeEl) {
        var p = modeEl.options[modeEl.selectedIndex];
        pathEl.textContent = "без ключа · " + ((p && p.getAttribute("data-path")) || "");
      }
      var temp = document.getElementById("chat-temp");
      var max = document.getElementById("chat-max");
      if (temp) temp.hidden = true;
      if (max) max.hidden = true;
      if (refBtn) refBtn.hidden = mode === "chat";
      if (mode === "chat") clearRefs();
      else renderRefs();
      loadModelParams();
    }
    if (modeEl) {
      modeEl.addEventListener("change", filterModels);
    }
    if (modelEl) {
      modelEl.addEventListener("change", function () {
        localStorage.setItem("ml-chat-model", modelEl.value);
        syncModeToModel();
      });
    }

    function clearRefs() {
      refs = [];
      renderRefs();
      if (refInput) refInput.value = "";
    }
    function renderRefs() {
      if (!refStrip) return;
      refStrip.innerHTML = "";
      if (!refs.length || genMode() === "chat") {
        refStrip.hidden = true;
        return;
      }
      refStrip.hidden = false;
      refs.forEach(function (src, i) {
        var wrap = document.createElement("div");
        wrap.className = "ref-thumb";
        var im = document.createElement("img");
        im.src = src;
        im.alt = "референс " + (i + 1);
        var rm = document.createElement("button");
        rm.type = "button";
        rm.className = "ghost";
        rm.textContent = "×";
        rm.title = "Убрать";
        rm.addEventListener("click", function () {
          refs.splice(i, 1);
          renderRefs();
        });
        wrap.appendChild(im);
        wrap.appendChild(rm);
        refStrip.appendChild(wrap);
      });
      markParamDrop();
    }
    var PARAM_LABELS = {
      input_image: "Референс", input_images: "Референсы", input_video: "Видео-референс",
      seconds: "Секунды", size: "Размер", seed: "Seed", width: "Ширина", height: "Высота",
      fps: "FPS", temperature: "temperature", max_tokens: "max_tokens",
      negative_prompt: "Негатив", n: "Количество"
    };
    function paramLabel(name) { return PARAM_LABELS[name] || name; }
    function skipParam(name) {
      return name === "prompt" || name === "model" || name === "input_images" ||
        name === "input_reference" || name === "input_references" || name === "n";
    }
    function isImageParam(p) {
      return p && (p.type === "image" || p.name === "input_image" || p.name === "input_video");
    }
    function requiredList(schema) {
      return ((schema && schema.required_parameters) || []).map(String);
    }
    function needsInputImage(schema) {
      var r = requiredList(schema);
      return r.indexOf("input_image") >= 0 || r.indexOf("input_images") >= 0 || r.indexOf("input_video") >= 0;
    }
    function loadModelParams() {
      var fields = document.getElementById("chat-params-fields");
      var emptyP = document.getElementById("chat-params-empty");
      var model = modelEl && modelEl.value;
      if (!model) {
        paramsSchema = null;
        if (fields) { fields.hidden = true; fields.innerHTML = ""; }
        if (emptyP) { emptyP.hidden = false; emptyP.textContent = "Выберите модель."; }
        return;
      }
      var key = genMode() + "|" + model;
      if (paramsCache[key]) {
        renderParams(paramsCache[key]);
        return;
      }
      var req = ++paramsReq;
      if (emptyP) { emptyP.hidden = false; emptyP.textContent = "загрузка схемы…"; }
      fetch("/admin/model-params?model=" + encodeURIComponent(model), {
        credentials: "same-origin",
        headers: { Accept: "application/json" }
      }).then(function (r) { return r.json(); }).then(function (j) {
        if (req !== paramsReq) return;
        paramsCache[key] = j || {};
        renderParams(paramsCache[key]);
      }).catch(function () {
        if (req !== paramsReq) return;
        renderParams({ modality: genMode() === "chat" ? "chat" : genMode(), parameters: [], required_parameters: [] });
      });
    }
    function renderParams(schema) {
      paramsSchema = schema || {};
      var fields = document.getElementById("chat-params-fields");
      var emptyP = document.getElementById("chat-params-empty");
      if (!fields) return;
      var params = paramsSchema.parameters || [];
      var required = requiredList(paramsSchema);
      fields.innerHTML = "";
      var shown = 0;
      var sawImage = false;
      params.forEach(function (p) {
        if (!p || !p.name || skipParam(p.name)) return;
        if (isImageParam(p) && sawImage) return;
        if (isImageParam(p)) sawImage = true;
        shown++;
        var wrap = document.createElement("div");
        wrap.className = "chat-param";
        var lab = document.createElement("label");
        lab.textContent = paramLabel(p.name);
        if (p.required || required.indexOf(p.name) >= 0) {
          var star = document.createElement("span");
          star.className = "req";
          star.textContent = "обязательно";
          lab.appendChild(star);
        }
        wrap.appendChild(lab);
        if (isImageParam(p)) {
          var drop = document.createElement("div");
          drop.className = "chat-param-drop";
          drop.id = "chat-param-drop";
          drop.textContent = "Нажмите или перетащите фото";
          if (p.required || required.indexOf(p.name) >= 0) drop.classList.add("is-required");
          drop.addEventListener("click", function () { if (refInput) refInput.click(); });
          drop.addEventListener("dragover", function (ev) { ev.preventDefault(); });
          drop.addEventListener("drop", function (ev) {
            ev.preventDefault();
            if (ev.dataTransfer && ev.dataTransfer.files) addRefFiles(ev.dataTransfer.files);
          });
          wrap.appendChild(drop);
        } else {
          wrap.appendChild(paramInput(p));
        }
        fields.appendChild(wrap);
      });
      fields.hidden = shown === 0;
      if (emptyP) {
        emptyP.hidden = shown > 0;
        if (shown === 0) emptyP.textContent = "У этой модели нет дополнительных полей.";
      }
      markParamDrop();
    }
    function paramInput(p) {
      var def = p.default;
      var tempEl = document.getElementById("chat-temp");
      var maxEl = document.getElementById("chat-max");
      if (p.name === "temperature" && tempEl && tempEl.value) def = tempEl.value;
      if (p.name === "max_tokens" && maxEl && maxEl.value) def = maxEl.value;
      if (p.enum && p.enum.length) {
        var sel = document.createElement("select");
        sel.setAttribute("data-param", p.name);
        sel.setAttribute("data-type", p.type || "string");
        p.enum.forEach(function (ev) {
          var o = document.createElement("option");
          o.value = String(ev);
          o.textContent = String(ev);
          if (def != null && String(ev) === String(def)) o.selected = true;
          sel.appendChild(o);
        });
        return sel;
      }
      var inp = document.createElement("input");
      inp.setAttribute("data-param", p.name);
      inp.setAttribute("data-type", p.type || "string");
      var t = p.type || "string";
      if (t === "integer" || t === "int" || t === "number" || t === "float") {
        inp.type = "number";
        inp.step = (t === "number" || t === "float") ? "any" : "1";
        if (p.min != null) inp.min = p.min;
        if (p.max != null) inp.max = p.max;
      } else if (t === "boolean" || t === "bool") {
        inp.type = "checkbox";
        inp.checked = def === true || def === "true";
        return inp;
      } else {
        inp.type = "text";
      }
      if (def != null && inp.type !== "checkbox") inp.value = String(def);
      return inp;
    }
    function markParamDrop() {
      var drop = document.getElementById("chat-param-drop");
      if (!drop) return;
      drop.classList.toggle("has-file", refs.length > 0);
      drop.textContent = refs.length
        ? ("фото: " + refs.length)
        : (drop.classList.contains("is-required") ? "Нужен референс — нажмите или перетащите" : "Нажмите или перетащите фото");
    }
    function collectExtraParams() {
      var out = {};
      var root = document.getElementById("chat-params-fields");
      if (!root) return out;
      root.querySelectorAll("[data-param]").forEach(function (el) {
        var name = el.getAttribute("data-param");
        var typ = el.getAttribute("data-type") || "";
        if (!name) return;
        if (el.type === "checkbox") {
          out[name] = el.checked;
          return;
        }
        var v = (el.value || "").trim();
        if (v === "") return;
        if (typ === "integer" || typ === "int") {
          var n = parseInt(v, 10);
          if (!isNaN(n)) out[name] = n;
          return;
        }
        if (typ === "number" || typ === "float") {
          var f = parseFloat(v);
          if (!isNaN(f)) out[name] = f;
          return;
        }
        out[name] = v;
      });
      return out;
    }
    function fileToDataURL(file) {
      return new Promise(function (resolve, reject) {
        if (!file || !(file.type || "").match(/^image\//)) {
          reject(new Error("нужен файл изображения"));
          return;
        }
        var url = URL.createObjectURL(file);
        var img = new Image();
        img.onload = function () {
          var w = img.naturalWidth || 1, h = img.naturalHeight || 1;
          var max = 1280;
          var scale = Math.min(1, max / Math.max(w, h));
          var c = document.createElement("canvas");
          c.width = Math.max(1, Math.round(w * scale));
          c.height = Math.max(1, Math.round(h * scale));
          c.getContext("2d").drawImage(img, 0, 0, c.width, c.height);
          URL.revokeObjectURL(url);
          resolve(c.toDataURL("image/jpeg", 0.84));
        };
        img.onerror = function () {
          URL.revokeObjectURL(url);
          reject(new Error("не удалось прочитать изображение"));
        };
        img.src = url;
      });
    }
    function addRefFiles(fileList) {
      var files = Array.prototype.slice.call(fileList || []);
      var room = MAX_REFS - refs.length;
      if (room <= 0) {
        if (status) status.textContent = "не больше " + MAX_REFS + " референсов";
        return Promise.resolve();
      }
      files = files.slice(0, room);
      return Promise.all(files.map(function (f) { return fileToDataURL(f); })).then(function (urls) {
        urls.forEach(function (u) { if (u) refs.push(u); });
        renderRefs();
      }).catch(function (e) {
        if (status) status.textContent = e.message || "не удалось прикрепить";
      });
    }
    function appendRefsToBubble(b, urls) {
      if (!b || !b.body || !urls || !urls.length) return;
      var row = document.createElement("div");
      row.className = "ref-row";
      urls.forEach(function (src) {
        var im = document.createElement("img");
        im.src = src;
        im.alt = "референс";
        row.appendChild(im);
      });
      b.body.appendChild(row);
    }
    function nearBottom() {
      return log.scrollHeight - log.scrollTop - log.clientHeight < 96;
    }
    function stickBottom(force) {
      if (force || pinBottom) log.scrollTop = log.scrollHeight;
    }
    log.addEventListener("scroll", function () {
      pinBottom = nearBottom();
    });

    function persist() {
      function pack(withImage) {
        return JSON.stringify({
          model: modelEl && modelEl.value,
          mode: genMode(),
          messages: messages.map(function (m) {
            var row = { role: m.role, content: typeof m.content === "string" ? m.content : "", think: m.think || "", hadImage: !!(m.image || m.hadImage) };
            if (withImage && m.image) row.image = m.image;
            return row;
          })
        });
      }
      try {
        sessionStorage.setItem("ml-chat-thread", pack(true));
      } catch (e) {
        try { sessionStorage.setItem("ml-chat-thread", pack(false)); } catch (e2) {}
      }
      renderHist();
    }
    function renderHist() {
      var task = document.getElementById("chat-task");
      var list = document.getElementById("chat-hist-list");
      var emptyH = document.getElementById("chat-hist-empty");
      if (!list) return;
      list.innerHTML = "";
      var first = "";
      messages.forEach(function (m) {
        if (m.role === "user" && !first) first = String(m.content || "");
      });
      if (task) {
        task.hidden = !first;
        task.textContent = first ? ("Задача: " + first) : "";
      }
      if (emptyH) emptyH.hidden = messages.length > 0;
      messages.forEach(function (m, i) {
        var li = document.createElement("li");
        var t = typeof m.content === "string" ? m.content : "(медиа)";
        if (m.hadImage || m.image) t = "изображение";
        li.textContent = (m.role === "user" ? "Вы: " : "Модель: ") + t.slice(0, 80);
        if (i === 0 && m.role === "user") li.className = "is-task";
        list.appendChild(li);
      });
      if (input) {
        input.placeholder = messages.length
          ? "Правка к этой генерации…"
          : "Напишите сообщение… Enter — отправить";
      }
    }
    function historyForAPI() {
      return messages.map(function (m) {
        if (m.role === "assistant" && m.image) {
          return {
            role: "assistant",
            content: [
              { type: "text", text: m.content || "Generated image." },
              { type: "image_url", image_url: { url: m.image } }
            ]
          };
        }
        return { role: m.role, content: m.content };
      });
    }
    function restore() {
      try {
        var raw = sessionStorage.getItem("ml-chat-thread");
        if (!raw) return;
        var data = JSON.parse(raw);
        if (!data || !data.messages || !data.messages.length) return;
        messages = data.messages;
        if (modeEl && data.mode) modeEl.value = data.mode;
        filterModels();
        messages.forEach(function (m) {
          var text = m.content || (m.hadImage ? "изображение" : "");
          var b = bubble(m.role === "user" ? "user" : "assistant", text, true);
          if (m.think && b.think) {
            b.think.hidden = false;
            b.think.textContent = m.think;
          }
          if (m.image) {
            b.body.textContent = "";
            var img = document.createElement("img");
            img.src = m.image;
            img.alt = text;
            b.body.appendChild(img);
          }
        });
        stickBottom(true);
        renderHist();
        syncModeToModel();
      } catch (e) {}
    }

    function bubble(role, text, skipScroll) {
      if (empty) empty.hidden = true;
      var div = document.createElement("div");
      div.className = "chat-msg " + role;
      var who = document.createElement("div");
      who.className = "who";
      who.textContent = role === "user" ? "Вы" : "Модель";
      div.appendChild(who);
      var thinkEl = null;
      if (role === "assistant") {
        thinkEl = document.createElement("div");
        thinkEl.className = "think";
        thinkEl.hidden = true;
        div.appendChild(thinkEl);
      }
      var body = document.createElement("div");
      body.className = "txt";
      body.textContent = text || "";
      div.appendChild(body);
      log.appendChild(div);
      if (!skipScroll) stickBottom(true);
      return { div: div, body: body, think: thinkEl };
    }

    function setBusy(on) {
      send.disabled = on;
      send.hidden = on;
      stop.hidden = !on;
      input.disabled = on;
    }

    document.getElementById("chat-clear").addEventListener("click", function () {
      if (ac) ac.abort();
      messages = [];
      log.querySelectorAll(".chat-msg").forEach(function (n) { n.remove(); });
      if (empty) empty.hidden = false;
      status.textContent = "";
      setBusy(false);
      clearRefs();
      try { sessionStorage.removeItem("ml-chat-thread"); } catch (e) {}
      renderHist();
    });
    if (refBtn && refInput) {
      refBtn.addEventListener("click", function () { refInput.click(); });
      refInput.addEventListener("change", function () {
        addRefFiles(refInput.files).then(function () { refInput.value = ""; });
      });
    }
    if (form) {
      form.addEventListener("dragover", function (ev) {
        if (genMode() === "chat") return;
        ev.preventDefault();
      });
      form.addEventListener("drop", function (ev) {
        if (genMode() === "chat") return;
        ev.preventDefault();
        if (ev.dataTransfer && ev.dataTransfer.files) addRefFiles(ev.dataTransfer.files);
      });
    }
    if (input) {
      input.addEventListener("paste", function (ev) {
        if (genMode() === "chat") return;
        var items = ev.clipboardData && ev.clipboardData.items;
        if (!items) return;
        var files = [];
        for (var i = 0; i < items.length; i++) {
          if (items[i].type && items[i].type.indexOf("image/") === 0) {
            var f = items[i].getAsFile();
            if (f) files.push(f);
          }
        }
        if (files.length) {
          ev.preventDefault();
          addRefFiles(files);
        }
      });
    }

    stop.addEventListener("click", function () {
      if (ac) ac.abort();
    });

    input.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" && !ev.shiftKey) {
        ev.preventDefault();
        form.requestSubmit();
      }
    });

    function downloadURL(url, name) {
      function clickBlob(href) {
        var a = document.createElement("a");
        a.href = href;
        a.download = name;
        a.rel = "noopener";
        document.body.appendChild(a);
        a.click();
        a.remove();
      }
      if ((url || "").indexOf("data:") === 0) {
        clickBlob(url);
        return;
      }
      fetch(url, { credentials: "same-origin" }).then(function (r) {
        if (!r.ok) throw new Error("download " + r.status);
        return r.blob();
      }).then(function (b) {
        var href = URL.createObjectURL(b);
        clickBlob(href);
        setTimeout(function () { URL.revokeObjectURL(href); }, 4000);
      }).catch(function (e) {
        if (status) status.textContent = e.message || "не удалось скачать";
      });
    }
    function mediaActions(asst, kind, src, prompt) {
      asst.div.classList.add("has-media");
      var old = asst.div.querySelector(".chat-actions");
      if (old) old.remove();
      var row = document.createElement("div");
      row.className = "chat-actions";
      var dl = document.createElement("button");
      dl.type = "button";
      dl.className = "ghost";
      dl.textContent = "Скачать";
      dl.addEventListener("click", function () {
        downloadURL(src, "mikrollm-" + kind + "-" + Date.now() + (kind === "video" ? ".mp4" : ".png"));
      });
      var rg = document.createElement("button");
      rg.type = "button";
      rg.className = "ghost";
      rg.textContent = "Ещё раз";
      rg.addEventListener("click", function () {
        if (send.disabled) return;
        if (messages.length && messages[messages.length - 1].role === "assistant") messages.pop();
        asst.body.textContent = "";
        asst.body.querySelectorAll("img,video").forEach(function (n) { n.remove(); });
        row.remove();
        runMedia(kind, prompt, asst);
      });
      row.appendChild(dl);
      row.appendChild(rg);
      asst.div.appendChild(row);
    }
    function videoJobStatus(j) {
      if (!j || typeof j !== "object") return "";
      var s = j.status || j.state;
      if (!s && j.data && typeof j.data === "object") s = j.data.status || j.data.state;
      return String(s || "").toLowerCase();
    }
    function pollVideo(id, model, asst, prompt) {
      var delay = 2500;
      var t0 = Date.now();
      var maxWait = 45 * 60 * 1000;
      function tick() {
        if (ac && ac.signal.aborted) {
          var e = new Error("остановлено");
          e.name = "AbortError";
          return Promise.reject(e);
        }
        if (Date.now() - t0 > maxWait) {
          return Promise.reject(new Error("видео всё ещё не готово (ждали 45 мин)"));
        }
        return fetch("/admin/videos/" + encodeURIComponent(id) + "?model=" + encodeURIComponent(model), {
          credentials: "same-origin",
          signal: ac ? ac.signal : undefined,
          headers: { "X-CSRF-Token": csrfToken() }
        }).then(function (r) {
          return r.json().then(function (j) { return { ok: r.ok, j: j }; }).catch(function () {
            return { ok: false, j: {} };
          });
        }).then(function (x) {
          var j = x.j || {};
          var st = videoJobStatus(j);
          var sec = Math.round((Date.now() - t0) / 1000);
          asst.body.textContent = "видео " + id + " · " + (st || "ожидание") + " · " + sec + " с";
          if (status) status.textContent = "ожидание генерации… " + sec + " с · Стоп отменяет опрос";
          stickBottom();
          if (st === "completed" || st === "complete" || st === "succeeded" || st === "success") {
            asst.body.textContent = "";
            var src = "/admin/videos/" + encodeURIComponent(id) + "/content?model=" + encodeURIComponent(model);
            var v = document.createElement("video");
            v.controls = true;
            v.src = src;
            asst.body.appendChild(v);
            mediaActions(asst, "video", src, prompt);
            return;
          }
          if (st === "failed" || st === "error" || st === "cancelled" || st === "canceled" || j.error) {
            var em = (j.error && (j.error.message || j.error)) || j.message || "генерация не удалась";
            var fe = new Error(typeof em === "string" ? em : JSON.stringify(em));
            fe.fatal = true;
            throw fe;
          }
          delay = Math.min(10000, delay + 400);
          return new Promise(function (res) { setTimeout(res, delay); }).then(tick);
        }).catch(function (e) {
          if (e && e.name === "AbortError") throw e;
          if (e && e.fatal) throw e;
          delay = Math.min(10000, delay + 400);
          asst.body.textContent = "видео " + id + " · повтор запроса статуса…";
          return new Promise(function (res) { setTimeout(res, delay); }).then(tick);
        });
      }
      return tick();
    }
    function runMedia(mode, text, asst) {
      setBusy(true);
      status.textContent = "генерация…";
      ac = new AbortController();
      var model = modelEl && modelEl.value;
      var url = mode === "image" ? "/admin/images" : "/admin/videos";
      var extra = collectExtraParams();
      var payload = { model: model, prompt: text, messages: historyForAPI() };
      Object.keys(extra).forEach(function (k) { payload[k] = extra[k]; });
      if (payload.n == null) payload.n = 1;
      if (!payload.size) payload.size = mode === "video" ? "720x1280" : "1024x1024";
      if (mode === "video" && (payload.seconds == null || payload.seconds === "")) payload.seconds = "4";
      if (refs.length) {
        payload.input_image = refs[0];
        payload.input_images = refs.slice();
        payload.input_references = refs.map(function (u) {
          return { type: "image_url", image_url: { url: u } };
        });
      }
      fetch(url, {
        method: "POST",
        credentials: "same-origin",
        signal: ac.signal,
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken() },
        body: JSON.stringify(payload)
      }).then(function (r) {
        return r.json().then(function (j) { return { ok: r.ok, j: j }; });
      }).then(function (x) {
        if (!x.ok) throw new Error((x.j && x.j.error && x.j.error.message) || JSON.stringify(x.j) || "error");
        if (mode === "image") {
          var d = (x.j.data && x.j.data[0]) || {};
          var src = d.b64_json ? ("data:image/png;base64," + d.b64_json) : (d.url || "");
          if (src) {
            asst.body.textContent = "";
            var img = document.createElement("img");
            img.src = src;
            img.alt = text;
            asst.body.appendChild(img);
            messages.push({ role: "assistant", content: "Изображение сгенерировано.", image: src });
            mediaActions(asst, "image", src, text);
          } else {
            asst.body.textContent = JSON.stringify(x.j);
            messages.push({ role: "assistant", content: asst.body.textContent });
          }
        } else {
          if (!x.j.id) throw new Error("провайдер не вернул id задачи");
          asst.body.textContent = "видео " + x.j.id + " · " + (x.j.status || "ожидание");
          status.textContent = "ожидание генерации…";
          return pollVideo(x.j.id, model, asst, text).then(function () {
            messages.push({ role: "assistant", content: "Видео сгенерировано." });
            persist();
            setBusy(false);
            status.textContent = "";
          });
        }
        persist();
        setBusy(false);
        status.textContent = "";
      }).catch(function (e) {
        if (e && e.name === "AbortError") {
          asst.body.textContent = asst.body.textContent || "остановлено";
          status.textContent = "остановлено";
          persist();
          setBusy(false);
          return;
        }
        asst.body.textContent = String(e.message || e);
        messages.push({ role: "assistant", content: asst.body.textContent });
        persist();
        setBusy(false);
        status.textContent = "";
      });
    }

    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var model = modelEl && modelEl.value;
      var text = (input.value || "").trim();
      if (!model) return;
      var mode = genMode();
      var mk = mediaKind(optionMedia());
      if (mk !== "chat" && mode === "chat") {
        mode = mk;
        if (modeEl) modeEl.value = mk;
        filterModels();
      }
      var shot = refs.slice();
      if (!text && !(shot.length && (mode === "image" || mode === "video"))) return;
      if ((mode === "image" || mode === "video") && needsInputImage(paramsSchema) && !shot.length) {
        if (status) status.textContent = "нужен референс (input_image)";
        return;
      }
      if (!text) text = "по референсам";
      input.value = "";
      messages.push({ role: "user", content: text });
      persist();
      var userB = bubble("user", text);
      if (mode === "image" || mode === "video") appendRefsToBubble(userB, shot);
      var asst = bubble("assistant", "");
      if (mode === "image" || mode === "video") {
        runMedia(mode, text, asst);
        clearRefs();
        return;
      }
      var acc = "";
      var think = "";
      setBusy(true);
      status.textContent = "генерация…";
      ac = new AbortController();
      var t0 = Date.now();
      fetch("/admin/chat", {
        method: "POST",
        credentials: "same-origin",
        signal: ac.signal,
        headers: { "Content-Type": "application/json", "Accept": "text/event-stream", "X-CSRF-Token": csrfToken() },
        body: JSON.stringify({
          model: model,
          stream: true,
          temperature: (function () {
            var extra = collectExtraParams();
            if (extra.temperature != null) return extra.temperature;
            return parseFloat(document.getElementById("chat-temp").value) || 0.7;
          })(),
          max_tokens: (function () {
            var extra = collectExtraParams();
            if (extra.max_tokens != null) return extra.max_tokens;
            return parseInt(document.getElementById("chat-max").value, 10) || 1024;
          })(),
          messages: messages
        })
      }).then(function (r) {
        if (!r.ok) {
          return r.text().then(function (t) { throw new Error(t || r.status); });
        }
        if (!r.body) throw new Error("нет потока");
        var reader = r.body.getReader();
        var dec = new TextDecoder();
        var buf = "";
        function pump() {
          return reader.read().then(function (x) {
            if (x.done) return;
            buf += dec.decode(x.value, { stream: true });
            var parts = buf.split("\n");
            buf = parts.pop();
            parts.forEach(function (line) {
              line = line.replace(/\r$/, "").trim();
              if (!line || line === "data: [DONE]") return;
              if (line.indexOf("data:") === 0) line = line.slice(5).trim();
              var j;
              try { j = JSON.parse(line); } catch (e) { return; }
              var ch = (j.choices && j.choices[0]) || {};
              var d = ch.delta || ch.message || {};
              if (d.reasoning) think += d.reasoning;
              if (d.reasoning_content) think += d.reasoning_content;
              if (d.content) acc += d.content;
              if (j.message && j.message.content && !d.content) acc = j.message.content;
              if (j.error) {
                var em = j.error.message || j.error;
                if (typeof em === "string") acc += em;
              }
              asst.body.textContent = acc;
              if (asst.think) {
                asst.think.hidden = !think;
                asst.think.textContent = think;
              }
              stickBottom();
            });
            return pump();
          });
        }
        return pump();
      }).then(function () {
        if (acc) messages.push({ role: "assistant", content: acc, think: think || undefined });
        else if (think) messages.push({ role: "assistant", content: think });
        status.textContent = ((Date.now() - t0) / 1000).toFixed(1) + " с";
        persist();
        stickBottom();
      }).catch(function (e) {
        if (e.name === "AbortError") {
          status.textContent = "остановлено";
          if (acc) messages.push({ role: "assistant", content: acc, think: think || undefined });
          persist();
          return;
        }
        status.textContent = e.message || "ошибка";
        asst.body.textContent = asst.body.textContent || (e.message || "ошибка");
      }).finally(function () {
        setBusy(false);
        ac = null;
        persist();
        stickBottom();
      });
    });
    restore();
    syncModeToModel();
  })();
})();
