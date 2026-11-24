/* BananaGit easter egg page, signed-out visitors only: shows the eggs found in this browser
 * (BananaEggs.localFinds(), from the shared banana-eggs.js, which loads before this deferred script). */
(function () {
  "use strict";
  function start() {
    var list = document.querySelector("[data-banana-eggs-page]");
    if (!list || !window.BananaEggs || typeof window.BananaEggs.localFinds !== "function") return;
    var finds = window.BananaEggs.localFinds();
    var found = 0;
    Array.prototype.forEach.call(list.querySelectorAll("[data-banana-eggs-id]"), function (row) {
      var when = finds[row.getAttribute("data-banana-eggs-id")];
      if (!when) return;
      found += 1;
      var date = new Date(when);
      var dateNode = row.querySelector("[data-banana-eggs-date]");
      if (dateNode) dateNode.textContent = isNaN(date.getTime()) ? "" : date.toLocaleDateString();
      var shown = row.querySelector("[data-banana-eggs-found]");
      var unknown = row.querySelector("[data-banana-eggs-unknown]");
      if (shown) shown.hidden = false;
      if (unknown) unknown.hidden = true;
    });
    var count = document.querySelector("[data-banana-eggs-count]");
    if (count) count.textContent = String(found);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
