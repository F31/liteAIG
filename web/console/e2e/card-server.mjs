// Serves a signed A2A Agent Card on 127.0.0.1:8094 plus the matching public
// key, so the Admin Console can run the discover -> verify -> review ->
// activate loop against a real signature. Signing mirrors internal/access/
// protocol/a2a/cardverify.go: RS256 JWS over the card's canonical JSON
// (marshaled WITHOUT the signature field), header {"alg":"RS256"}.
import {
  createHash,
  createSign,
  generateKeyPairSync,
} from "node:crypto";
import { createServer } from "node:http";

const HOST = "127.0.0.1";
const PORT = 8094;
const ORIGIN = `http://${HOST}:${PORT}`;

const b64url = (buf) =>
  Buffer.from(buf)
    .toString("base64")
    .replace(/=+$/g, "")
    .replace(/\+/g, "-")
    .replace(/\//g, "_");

// Field order must match the Go AgentCard struct exactly so the canonical
// bytes the signer covers equal what the verifier re-derives.
const cardFields = {
  name: "peer-agent",
  url: ORIGIN,
  description: "Playwright test peer agent",
  version: "1",
  skills: [],
  capabilities: ["chat"],
  publisher: "playwright",
};

const { privateKey, publicKey } = generateKeyPairSync("rsa", {
  modulusLength: 2048,
});
const header = JSON.stringify({ alg: "RS256" });
const canonical = JSON.stringify(cardFields);
const digest = createHash("sha256").update(canonical).digest();
const signingInput = `${b64url(header)}.${b64url(digest)}`;
const signature = createSign("RSA-SHA256")
  .update(signingInput)
  .end()
  .sign(privateKey);
const card = {
  ...cardFields,
  signature: { alg: "RS256", value: b64url(signature) },
};
const publicPem = publicKey.export({ type: "spki", format: "pem" });

createServer((req, res) => {
  if (req.url === "/healthz") {
    res.writeHead(200).end("ok");
    return;
  }
  if (req.url === "/.well-known/agent-card.json") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify(card));
    return;
  }
  if (req.url === "/key.pem") {
    res.writeHead(200, { "Content-Type": "application/x-pem-file" });
    res.end(publicPem);
    return;
  }
  res.writeHead(404).end();
}).listen(PORT, HOST, () => console.log(`card server on ${ORIGIN}`));
