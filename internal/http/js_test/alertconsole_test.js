"use strict";
// Tests for content/static/js/alertconsole_console.js, with a document and a request that are fakes. Run with:
// node js_test/alertconsole_test.js (the Go test in oldbrowsers_test.go does).
var assert = require("assert");
var fs = require("fs");
var script = require("../content/static/js/alertconsole_console.js");

var tests = [];
function test(name, fn) { tests.push([name, fn]); }

function fakePage() {
    var error = {textContent: ""};
    var button = {disabled: false, dataset: {source: "porkbun", key: "example.com/A/www"}, listeners: {},
        addEventListener: function (type, fn) { this.listeners[type] = fn; },
        parentNode: {querySelector: function () { return error; }}};
    var root = {querySelectorAll: function () { return [button]; }};

    return {button: button, error: error, doc: {querySelector: function () { return root; }}};
}

function console_(page) {
    var posts = [];
    var reloads = [];
    var c = new script.AlertConsole(page.doc, {
        post: function (url, body, done) { posts.push({url: url, body: body, done: done}); },
        reload: function () { reloads.push(1); }
    });

    return {c: c, posts: posts, reloads: reloads};
}

test("a click acknowledges the alert and loads the page again", function () {
    var page = fakePage();
    var t = console_(page);
    assert.strictEqual(t.c.init(), true);

    page.button.listeners.click();

    assert.strictEqual(t.posts[0].url, "/plugins/alertconsole/acknowledge");
    assert.deepStrictEqual(t.posts[0].body, {Source: "porkbun", Key: "example.com/A/www"});
    assert.strictEqual(page.button.disabled, true);

    t.posts[0].done(null, 200, "");
    assert.strictEqual(t.reloads.length, 1);
});

test("a refusal and no answer are said on the page, and the button comes back", function () {
    var page = fakePage();
    var t = console_(page);
    t.c.init();

    page.button.listeners.click();
    t.posts[0].done(null, 500, "boom");
    assert.strictEqual(page.error.textContent, "Could not acknowledge (500)");
    assert.strictEqual(page.button.disabled, false);

    page.button.listeners.click();
    t.posts[1].done(new Error("the request could not be sent"));
    assert.ok(page.error.textContent.indexOf("Could not acknowledge: ") === 0);
    assert.strictEqual(page.button.disabled, false);
    assert.strictEqual(t.reloads.length, 0);
});

test("init says whether there was a console on the page", function () {
    var c = new script.AlertConsole({querySelector: function () { return null; }}, {});
    assert.strictEqual(c.init(), false);
});

// The lint every script's test has: what Safari 9 does not have, looked for in the source. Pasted into each test.
test("the script uses nothing Safari 9 does not have", function () {
    var source = fs.readFileSync(__dirname + "/../content/static/js/alertconsole_console.js", "utf8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");

    [
        [/=>/, "an arrow function"], [/\bclass\s+\w/, "a class"], [/\blet\s/, "let"], [/\bconst\s/, "const"], [/`/, "a template literal"],
        [/\basync\b/, "async"], [/\bawait\b/, "await"], [/\bfetch\s*\(/, "fetch"], [/\.\.\./, "spread"], [/\?\./, "optional chaining"],
        [/\?\?/, "nullish coalescing"], [/\bObject\.(entries|values)\b/, "Object.entries or values"], [/\.padStart\b|\.padEnd\b/, "padStart"]
    ].forEach(function (pair) {
        assert.ok(!pair[0].test(source), "the script has " + pair[1]);
    });

    // forEach is only for arrays: a NodeList has none before Safari 10, which is what toArray is for.
    (source.match(/(\w+)\.forEach/g) || []).forEach(function (use) {
        assert.ok([].indexOf(use) >= 0, "forEach on " + use);
    });
});

var failed = 0;
tests.forEach(function (t) {
    try { t[1](); console.log("ok   " + t[0]); } catch (e) { failed++; console.log("FAIL " + t[0] + "\n     " + (e && e.stack || e)); }
});
if (failed) { process.exit(1); }
