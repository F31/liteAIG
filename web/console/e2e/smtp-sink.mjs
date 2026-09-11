// Minimal SMTP sink for the E2E suite. Speaks just enough SMTP for the Go
// net/smtp client (no auth, no TLS) and exposes captured messages over a
// small HTTP API so tests can read the one-time reset code.
//
//   - SMTP:  127.0.0.1:18025 (override with E2E_SMTP_PORT)
//   - HTTP:  127.0.0.1:18026 (override with E2E_SMTP_HTTP_PORT)
//     GET    /healthz           -> 200 {ok:true}
//     GET    /messages          -> [{to, subject, body}]
//     DELETE /messages          -> clears the captured messages
import { createServer } from "node:net";
import { createServer as createHttp } from "node:http";

const HOST = "127.0.0.1";
const SMTP_PORT = Number(process.env.E2E_SMTP_PORT || 18025);
const HTTP_PORT = Number(process.env.E2E_SMTP_HTTP_PORT || 18026);

const messages = [];

function reply(socket, code, text) {
  socket.write(`${code} ${text}\r\n`);
}

function parseMessage(raw) {
  const separator = raw.indexOf("\r\n\r\n");
  const headerText = separator === -1 ? raw : raw.slice(0, separator);
  const bodyText = separator === -1 ? "" : raw.slice(separator + 4);
  const headers = {};
  for (const line of headerText.split("\r\n")) {
    const idx = line.indexOf(":");
    if (idx !== -1) {
      headers[line.slice(0, idx).trim().toLowerCase()] =
        line.slice(idx + 1).trim();
    }
  }
  messages.push({
    to: headers.to || "",
    subject: headers.subject || "",
    body: bodyText.replace(/\r\n/g, "\n"),
  });
}

function handleSmtp(socket) {
  let buffer = "";
  let inData = false;
  let message = "";
  reply(socket, 220, "liteaig-e2e ESMTP ready");

  socket.on("data", (chunk) => {
    buffer += chunk.toString("binary");
    if (inData) {
      message += buffer;
      buffer = "";
      const terminator = message.indexOf("\r\n.\r\n");
      if (terminator !== -1) {
        parseMessage(message.slice(0, terminator));
        message = "";
        inData = false;
        reply(socket, 250, "queued");
      }
      return;
    }
    for (;;) {
      const nl = buffer.indexOf("\r\n");
      if (nl === -1) return; // wait for the rest of the command
      const line = buffer.slice(0, nl);
      buffer = buffer.slice(nl + 2);
      const upper = line.toUpperCase();
      if (upper.startsWith("EHLO") || upper.startsWith("HELO")) {
        reply(socket, 250, "liteaig-e2e");
      } else if (upper.startsWith("MAIL FROM")) {
        reply(socket, 250, "ok");
      } else if (upper.startsWith("RCPT TO")) {
        reply(socket, 250, "ok");
      } else if (upper.startsWith("DATA")) {
        reply(socket, 354, "end with <CRLF>.<CRLF>");
        inData = true;
      } else if (upper.startsWith("QUIT")) {
        reply(socket, 221, "bye");
        socket.end();
        return;
      } else {
        reply(socket, 250, "ok");
      }
    }
  });
  socket.on("error", () => {});
}

createServer(handleSmtp).listen(SMTP_PORT, HOST, () => {
  console.log(`smtp sink listening on ${HOST}:${SMTP_PORT}`);
});

createHttp((req, res) => {
  res.setHeader("Content-Type", "application/json");
  if (req.method === "GET" && req.url === "/healthz") {
    res.end(JSON.stringify({ ok: true }));
  } else if (req.method === "GET" && req.url === "/messages") {
    res.end(JSON.stringify(messages));
  } else if (req.method === "DELETE" && req.url === "/messages") {
    messages.length = 0;
    res.end(JSON.stringify({ ok: true }));
  } else {
    res.statusCode = 404;
    res.end(JSON.stringify({ error: "not found" }));
  }
}).listen(HTTP_PORT, HOST, () => {
  console.log(`smtp sink http on ${HOST}:${HTTP_PORT}`);
});
