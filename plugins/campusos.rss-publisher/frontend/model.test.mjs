import { test } from "node:test";
import assert from "node:assert/strict";
import {
  canonicalFeedURL,
  subscriptions,
  addSubscription,
  inertImages,
} from "./model.js";
test("bounded personal subscriptions reject duplicates, credentials, and non-HTTPS", () => {
  let values = addSubscription({}, "https://example.com/feed");
  assert.equal(subscriptions(values).length, 1);
  assert.throws(() => addSubscription(values, "https://example.com/feed"));
  for (const value of [
    "http://example.com/feed",
    "https://user:password@example.com/feed",
    "https://example.com/feed#fragment",
  ])
    assert.throws(() => canonicalFeedURL(value));
  for (let n = 2; n <= 6; n++)
    values = addSubscription(values, `https://example.com/${n}`);
  assert.throws(() => addSubscription(values, "https://example.com/seven"));
});
test("preview never puts an external image src into the document", () => {
  const safe = inertImages(
    '<p>body</p><img src="https://example.com/photo.jpg" alt="图片"><a href="https://example.com">source</a>',
  );
  assert.ok(safe.includes('data-rss-src="https://example.com/photo.jpg"'));
  assert.ok(!/<img[^>]*\ssrc=/.test(safe));
  assert.ok(safe.includes("<p>body</p>"));
});
