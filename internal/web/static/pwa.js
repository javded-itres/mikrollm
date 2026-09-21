(function () {
  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/admin/sw.js", { scope: "/admin/" }).catch(function () {});
  }
  var btn = document.getElementById("pwa-install");
  var deferred = null;
  window.addEventListener("beforeinstallprompt", function (e) {
    e.preventDefault();
    deferred = e;
    if (btn) btn.hidden = false;
  });
  window.addEventListener("appinstalled", function () {
    deferred = null;
    if (btn) btn.hidden = true;
  });
  if (btn) {
    btn.addEventListener("click", function () {
      if (!deferred) return;
      deferred.prompt();
      deferred.userChoice.finally(function () {
        deferred = null;
        btn.hidden = true;
      });
    });
  }
})();
