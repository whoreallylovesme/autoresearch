(() => {
  function initializeLogin() {
    const input = document.getElementById('operator-token');
    const toggle = document.getElementById('toggle-password');
    if (!(input instanceof HTMLInputElement) || !(toggle instanceof HTMLButtonElement)) return;
    toggle.addEventListener('click', () => {
      const showPassword = input.type === 'password';
      input.type = showPassword ? 'text' : 'password';
      toggle.setAttribute('aria-pressed', String(showPassword));
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initializeLogin, { once: true });
  } else {
    initializeLogin();
  }
})();
