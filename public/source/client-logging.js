var rootPath = (ungit.config && ungit.config.rootPath) || '';
var reporting = false;

function text(value, limit) {
  if (value === undefined || value === null) return '';
  var output;
  try {
    output = typeof value === 'string' ? value : JSON.stringify(value);
  } catch {
    output = String(value);
  }
  output = output
    .replace(/([a-z][a-z0-9+.-]*:\/\/)([^/@\s]+)@/gi, '$1<redacted>@')
    .replace(/([^\s/@:]+):([^\s/@]+)@/gi, '<redacted>@')
    .replace(
      /(password|passwd|token|access_token|api_key|authorization|cookie|secret)=([^&\s]+)/gi,
      '$1=<redacted>'
    );
  return output.length > limit ? output.slice(0, limit) + '…' : output;
}

function report(level, event, message, details, stack) {
  if (reporting) return;
  reporting = true;
  var payload = {
    level: level || 'error',
    event: text(event || 'browser.message', 128),
    message: text(message, 4096),
    stack: text(stack, 16384),
    // Hash routes can contain local repository paths; never send them to logs.
    page: window.location.pathname,
    socketId: ungit.server && ungit.server.socketId ? String(ungit.server.socketId) : '',
    timestamp: new Date().toISOString(),
    details: details || {},
  };
  fetch(rootPath + '/api/client-log', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    keepalive: true,
  })
    .catch(function () {})
    .finally(function () {
      reporting = false;
    });
}

module.exports = {
  report: report,
  text: text,
};
