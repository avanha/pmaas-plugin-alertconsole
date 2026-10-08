class AlertConsole {
    constructor(doc) {
        this.doc = doc;
    }

    init = () => {
        const root = this.doc.querySelector("div.entity-alertconsole-console");

        if (!root) {
            return false;
        }

        root.querySelectorAll("button.ack-button").forEach((button) => {
            button.addEventListener("click", () => this.acknowledge(button));
        });

        return true;
    }

    acknowledge = async (button) => {
        const errorElement = button.parentElement.querySelector(".ack-error");
        errorElement.textContent = "";
        button.disabled = true;

        try {
            const response = await fetch("/plugins/alertconsole/acknowledge", {
                method: "POST",
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({Source: button.dataset.source, Key: button.dataset.key}),
            });

            if (!response.ok) {
                // Say so on the page: a click that silently does nothing is worse than an error.
                console.error("Failed to acknowledge alert:", response.status, await response.text());
                errorElement.textContent = "Could not acknowledge (" + response.status + ")";
                button.disabled = false;
                return;
            }

            // Whether or not it was still there to acknowledge, the page is stale now.
            location.reload();
        } catch (error) {
            console.error("Failed to acknowledge alert:", error);
            errorElement.textContent = "Could not acknowledge: " + error;
            button.disabled = false;
        }
    }
}

new AlertConsole(document).init();
