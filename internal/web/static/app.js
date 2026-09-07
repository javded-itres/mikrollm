(function () {
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

  function bindModelFilters(nameId, provId, priceId, rowSel, countId) {
    var nameEl = document.getElementById(nameId);
    var provEl = document.getElementById(provId);
    var priceEl = document.getElementById(priceId);
    if (!nameEl && !provEl && !priceEl) return;
    function apply() {
      var q = (nameEl && nameEl.value ? nameEl.value : "").trim().toLowerCase();
      var prov = provEl ? provEl.value : "";
      var price = priceEl ? priceEl.value : "";
      var vis = 0, total = 0;
      document.querySelectorAll(rowSel).forEach(function (el) {
        total++;
        var name = (el.getAttribute("data-name") || "").toLowerCase();
        var title = (el.getAttribute("data-title") || "").toLowerCase();
        var provider = el.getAttribute("data-provider") || "";
        var ok = true;
        if (q && name.indexOf(q) < 0 && title.indexOf(q) < 0 && provider.toLowerCase().indexOf(q) < 0) ok = false;
        if (prov && provider !== prov) ok = false;
        if (!matchBand(el.getAttribute("data-band") || "none", el.getAttribute("data-prompt"), price)) ok = false;
        el.classList.toggle("is-hidden", !ok);
        if (ok) vis++;
      });
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
    [nameEl, provEl, priceEl].forEach(function (el) {
      if (!el) return;
      el.addEventListener("input", apply);
      el.addEventListener("change", apply);
    });
    apply();
  }
  bindModelFilters("model-filter", "model-provider", "model-price", ".model-tr", "catalog-count");
  bindModelFilters("key-model-filter", "key-provider", "key-price", "#key-models .pick", "key-count");

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
    }
  };
  var knownURLs = {
    "http://192.168.88.82:11434": 1,
    "http://192.168.88.82:8000": 1,
    "http://192.168.88.82:1234": 1,
    "https://openrouter.ai/api/v1": 1,
    "https://ollama.com": 1
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
        headers: { "Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded" },
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
        headers: { "Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded" },
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
          "<div class=\"job-title\">" + title + " · " + host + " · " + latest.model +
          (pct ? " · " + pct : "") + "</div>" +
          "<div class=\"muted\">" + (latest.error || latest.message || "") +
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
    var messages = [];
    var ac = null;
    var pinBottom = true;
    var saved = localStorage.getItem("ml-chat-model");
    if (saved && modelEl) {
      for (var i = 0; i < modelEl.options.length; i++) {
        if (modelEl.options[i].value === saved) { modelEl.selectedIndex = i; break; }
      }
    }
    if (modelEl) {
      modelEl.addEventListener("change", function () {
        localStorage.setItem("ml-chat-model", modelEl.value);
      });
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
      try {
        sessionStorage.setItem("ml-chat-thread", JSON.stringify({
          model: modelEl && modelEl.value,
          messages: messages
        }));
      } catch (e) {}
    }
    function restore() {
      try {
        var raw = sessionStorage.getItem("ml-chat-thread");
        if (!raw) return;
        var data = JSON.parse(raw);
        if (!data || !data.messages || !data.messages.length) return;
        messages = data.messages;
        messages.forEach(function (m) {
          var b = bubble(m.role === "user" ? "user" : "assistant", m.content || "", true);
          if (m.think && b.think) {
            b.think.hidden = false;
            b.think.textContent = m.think;
          }
        });
        stickBottom(true);
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
      try { sessionStorage.removeItem("ml-chat-thread"); } catch (e) {}
    });

    stop.addEventListener("click", function () {
      if (ac) ac.abort();
    });

    input.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" && !ev.shiftKey) {
        ev.preventDefault();
        form.requestSubmit();
      }
    });

    form.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var model = modelEl && modelEl.value;
      var text = (input.value || "").trim();
      if (!model || !text) return;
      input.value = "";
      messages.push({ role: "user", content: text });
      persist();
      bubble("user", text);
      var asst = bubble("assistant", "");
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
        headers: { "Content-Type": "application/json", "Accept": "text/event-stream" },
        body: JSON.stringify({
          model: model,
          stream: true,
          temperature: parseFloat(document.getElementById("chat-temp").value) || 0.7,
          max_tokens: parseInt(document.getElementById("chat-max").value, 10) || 1024,
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
  })();
})();
