export {};

const ADMIN_API_KEY = process.env.ADMIN_API_KEY || "";
const BASE_URL = process.env.BASE_URL || "http://localhost:8080";
const TEST_CHAT_ID = process.env.TEST_CHAT_ID || "0";
const TIMEOUT_MS = 10_000;

let failed = 0;

if (!ADMIN_API_KEY) {
  console.error("Error: ADMIN_API_KEY environment variable is required");
  console.error("Set it with: export ADMIN_API_KEY=your_key_here");
  process.exit(1);
}

async function testEndpoint(path: string): Promise<boolean> {
  console.log(`\nTesting: ${path}`);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);

  try {
    const response = await fetch(`${BASE_URL}${path}`, {
      headers: {
        "X-API-Key": ADMIN_API_KEY,
      },
      signal: controller.signal,
    });
    clearTimeout(timer);

    console.log(`  Status: ${response.status}`);
    console.log(`  Content-Type: ${response.headers.get("content-type")}`);

    if (!response.ok) {
      console.error(`  ❌ HTTP ${response.status} for ${path}`);
      failed++;
      return false;
    }

    const text = await response.text();
    console.log(`  Response length: ${text.length} chars`);

    if (text.length > 0) {
      try {
        const json = JSON.parse(text);
        console.log(`  ✅ Valid JSON response`);
        if (json.models) {
          console.log(`  Models count: ${json.models.length}`);
        }
      } catch {
        console.log(`  ❌ Not valid JSON: ${text.substring(0, 100)}...`);
      }
    }
    return true;
  } catch (e) {
    clearTimeout(timer);
    console.error(`  ❌ Error: ${e}`);
    failed++;
    return false;
  }
}

console.log("Testing Telegram Service API Calls...");
console.log("=====================================");

await testEndpoint("/api/v1/ai/models");
await testEndpoint("/api/v1/ai/models?provider=openai");
await testEndpoint(`/api/v1/ai/status/${TEST_CHAT_ID}`);
await testEndpoint("/api/v1/bootstrap/status");

console.log("\n=====================================");
console.log("Tests complete");

if (failed > 0) {
  console.error(`\n${failed} test(s) failed`);
  process.exit(1);
}
