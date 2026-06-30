// Aggiunge un toggle "mostra/nascondi password" (occhietto) ai campi password
// nei form principali. Nessuna dipendenza, vanilla JS.
document.addEventListener('DOMContentLoaded', function () {
  document.querySelectorAll('input[type=password]').forEach(function (inp) {
    // solo nei form ordinati (auth-card / formgrid), non nelle righe inline
    if (!inp.closest('.field') && !inp.closest('.formgrid')) return;

    var wrap = document.createElement('span');
    wrap.className = 'pw-wrap';
    inp.parentNode.insertBefore(wrap, inp);
    wrap.appendChild(inp);

    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'pw-eye';
    b.textContent = '\u{1F441}';
    b.title = 'Mostra/nascondi password';
    b.tabIndex = -1;
    b.addEventListener('click', function () {
      var show = inp.type === 'password';
      inp.type = show ? 'text' : 'password';
      b.textContent = show ? '\u{1F648}' : '\u{1F441}';
    });
    wrap.appendChild(b);
  });
});
