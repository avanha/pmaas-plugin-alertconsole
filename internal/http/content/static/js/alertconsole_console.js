/*
 * The acknowledge buttons of the alert console: a click acknowledges the alert, and the page is loaded again, whether or not it was still
 * there to acknowledge, since the page is stale now.
 *
 * It runs on old browsers, an iPad that no longer gets updates among them (Safari 9): plain ES5, with no arrow functions, classes, class
 * fields, let or const, async functions or fetch, none of which it has, and a script that uses one of them does not run at all, which is a
 * page of buttons that cannot be clicked. Nor does it have NodeList.forEach. Nothing here is slower or worse in a modern browser.
 *
 * The parts are given what they use from outside (the document, how a request is made, how the page is reloaded), so that they are tested
 * with fakes: see js_test/alertconsole_test.js.
 */
(function (global) {
    "use strict";

    // A NodeList from the old browsers has no forEach.
    function toArray(list) {
        return Array.prototype.slice.call(list);
    }

    // Posts the body as JSON, and calls done(error, status, text), error being set if there was no answer at all.
    function postJson(url, body, done) {
        var request = new global.XMLHttpRequest();

        request.open("POST", url, true);
        request.setRequestHeader("Content-Type", "application/json");

        request.onload = function () {
            done(null, request.status, request.responseText);
        };

        request.onerror = function () {
            done(new Error("the request could not be sent"));
        };

        request.send(JSON.stringify(body));
    }

    function AlertConsole(doc, options) {
        options = options || {};
        this.doc = doc;
        this.post = options.post || postJson;
        this.reload = options.reload || function () {
            global.location.reload();
        };
    }

    AlertConsole.prototype.init = function () {
        var self = this;
        var root = this.doc.querySelector("div.entity-alertconsole-console");

        if (!root) {
            return false;
        }

        toArray(root.querySelectorAll("button.ack-button")).forEach(function (button) {
            button.addEventListener("click", function () {
                self.acknowledge(button);
            });
        });

        return true;
    };

    AlertConsole.prototype.acknowledge = function (button) {
        var self = this;
        var errorElement = button.parentNode.querySelector(".ack-error");
        errorElement.textContent = "";
        button.disabled = true;

        this.post("/plugins/alertconsole/acknowledge", {Source: button.dataset.source, Key: button.dataset.key}, function (error, status, text) {
            if (error) {
                console.error("Failed to acknowledge alert:", error);
                errorElement.textContent = "Could not acknowledge: " + error;
                button.disabled = false;
                return;
            }

            if (status < 200 || status > 299) {
                // Say so on the page: a click that silently does nothing is worse than an error.
                console.error("Failed to acknowledge alert:", status, text);
                errorElement.textContent = "Could not acknowledge (" + status + ")";
                button.disabled = false;
                return;
            }

            // Whether or not it was still there to acknowledge, the page is stale now.
            self.reload();
        });
    };

    var exported = {AlertConsole: AlertConsole};

    if (typeof module !== "undefined" && module.exports) {
        module.exports = exported;
    }

    if (typeof document !== "undefined" && global.addEventListener) {
        new AlertConsole(document).init();
    }
})(typeof window !== "undefined" ? window : globalThis);
