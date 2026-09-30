"use strict";
// Overview: reload every 10 s unless the user is typing or a form is open.
// Pairing: wait for the code to be used or to expire.
(function () {
  var pairing = document.getElementById("pairing");
  if (!pairing) {
    if (document.querySelector("[data-refresh]")) {
      setInterval(function () {
        var busy = document.activeElement && /INPUT|BUTTON/.test(document.activeElement.tagName);
        if (!busy && !document.querySelector("details[open]")) location.reload();
      }, 10000);
    }
    return;
  }
  var code = pairing.getAttribute("data-code");
  var state = document.getElementById("pair-state");
  var revoke = document.getElementById("revoke");
  function show(text, cls) {
    state.textContent = text;
    state.className = "state " + cls;
    if (revoke) revoke.hidden = true;
  }
  function poll() {
    fetch("pair/" + encodeURIComponent(code) + "/state?wait=25", { credentials: "same-origin" })
      .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
      .then(function (st) {
        if (st.used) {
          var d = st.device || {};
          show(state.getAttribute("data-paired") + ": " + (d.name || "") + (d.keyFingerprint ? " (" + d.keyFingerprint + "). " : ". ") + state.getAttribute("data-check"), "ok");
        } else if (st.expired) {
          show(state.getAttribute("data-expired"), "bad");
        } else {
          poll();
        }
      })
      .catch(function () { setTimeout(poll, 3000); });
  }
  poll();
})();
