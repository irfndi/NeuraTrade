export {};

const ADMIN_API_KEY = process.env.ADMIN_API_KEY || "";
const BASE_URL = process.env.BASE_URL || "http://localhost:8080";

if (!ADMIN_API_KEY) {
  console.error("Error: ADMIN_API_KEY environment variable is required");
  console.error("Set it with: export ADMIN_API_KEY=your_key_here");
  process.exit(1);
}

async function testEndpoint(path: string) {
  console.log(`\nTesting: ${path}`);
  try {
    const response = await fetch(`${BASE_URL}${path}`, {
      headers: {
        "X-API-Key": ADMIN_API_KEY,
      },
    });

    console.log(`  Status: ${response.status}`);
    console.log(`  Content-Type: ${response.headers.get("content-type")}`);

    const text = await response.text();
    console.log(`  Response length: ${text.length} chars`);
    
    if (text.length > 0) {
      try {
        const json = JSON.parse(text);
        console.log(`  ✅ Valid JSON response`);
        if (json.models) {
          console.log(`  Models count: ${json.models.length}`);
        }
      } catch (e) {
        console.log(`  ❌ Not valid JSON: ${text.substring(0, 100)}...`);
      }
    }
  } catch (e) {
    console.log(`  ❌ Error: ${e}`);
  }
}

console.log("Testing Telegram Service API Calls...");
console.log("=====================================");

await testEndpoint("/api/v1/ai/models");
await testEndpoint("/api/v1/ai/models?provider=openai");
await testEndpoint("/api/v1/ai/status/1082762347");
await testEndpoint("/api/v1/bootstrap/status");

console.log("\n=====================================");
console.log("Tests complete");
