"use strict";
// Overview: reload every 10 s unless the user is typing or a form is open.
// Pairing: wait for the code to be used or to expire.
(function () {
  // Copy buttons: the field next to the button. Inside the ingress frame
  // the clipboard API may be blocked; then the text stays selected.
  document.querySelectorAll("[data-copy]").forEach(function (button) {
    button.addEventListener("click", function () {
      var field = button.parentNode.querySelector("input");
      field.focus();
      field.select();
      var done = function () { button.textContent = button.getAttribute("data-done"); };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(field.value).then(done, function () {
          if (document.execCommand("copy")) done();
        });
      } else if (document.execCommand("copy")) {
        done();
      }
    });
  });
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
