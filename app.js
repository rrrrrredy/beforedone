"use strict";

const receiptToggle = document.querySelector("[data-receipt-toggle]");
const passReceipt = document.querySelector('[data-receipt="pass"]');
const staleReceipt = document.querySelector('[data-receipt="stale"]');

if (receiptToggle && passReceipt && staleReceipt) {
  receiptToggle.addEventListener("click", () => {
    const showStale = receiptToggle.getAttribute("aria-pressed") !== "true";
    receiptToggle.setAttribute("aria-pressed", String(showStale));
    receiptToggle.textContent = showStale ? "Show current state" : "Show stale state";
    passReceipt.hidden = showStale;
    staleReceipt.hidden = !showStale;
  });
}

document.querySelectorAll("[data-tabs]").forEach((tabs) => {
  const tabButtons = [...tabs.querySelectorAll('[role="tab"]')];
  const panels = [...tabs.querySelectorAll('[role="tabpanel"]')];

  const selectTab = (button, moveFocus = true) => {
    const selectedName = button.dataset.tab;

    tabButtons.forEach((candidate) => {
      const selected = candidate === button;
      candidate.setAttribute("aria-selected", String(selected));
      candidate.tabIndex = selected ? 0 : -1;
    });

    panels.forEach((panel) => {
      panel.hidden = panel.dataset.panel !== selectedName;
    });

    if (moveFocus) {
      button.focus();
    }
  };

  tabButtons.forEach((button, index) => {
    button.addEventListener("click", () => selectTab(button, false));
    button.addEventListener("keydown", (event) => {
      let nextIndex;

      if (event.key === "ArrowRight" || event.key === "ArrowDown") {
        nextIndex = (index + 1) % tabButtons.length;
      } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
        nextIndex = (index - 1 + tabButtons.length) % tabButtons.length;
      } else if (event.key === "Home") {
        nextIndex = 0;
      } else if (event.key === "End") {
        nextIndex = tabButtons.length - 1;
      } else {
        return;
      }

      event.preventDefault();
      selectTab(tabButtons[nextIndex]);
    });
  });
});

const copyText = async (value) => {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const input = document.createElement("textarea");
  input.value = value;
  input.setAttribute("readonly", "");
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.appendChild(input);
  input.select();
  const copied = document.execCommand("copy");
  input.remove();

  if (!copied) {
    throw new Error("Copy command was not accepted");
  }
};

document.querySelectorAll("[data-copy]").forEach((button) => {
  button.addEventListener("click", async () => {
    const originalLabel = button.textContent;
    const target = button.dataset.copyTarget
      ? document.querySelector(button.dataset.copyTarget)
      : null;
    const value = target?.textContent || button.dataset.copy || "";

    try {
      await copyText(value);
      button.textContent = "Copied";
    } catch {
      button.textContent = "Select text";
      const code = button.parentElement?.querySelector("code");
      const selection = window.getSelection();
      if (code && selection) {
        const range = document.createRange();
        range.selectNodeContents(code);
        selection.removeAllRanges();
        selection.addRange(range);
      }
    }

    window.setTimeout(() => {
      button.textContent = originalLabel;
    }, 1800);
  });
});

document.querySelectorAll("[data-readme-source]").forEach(async (target) => {
  try {
    const response = await fetch(target.dataset.readmeSource, { cache: "no-cache" });
    if (!response.ok) {
      throw new Error(`README request failed with ${response.status}`);
    }
    target.textContent = await response.text();
  } catch {
    target.textContent = "The full README could not be loaded here. Open it in the GitHub repository instead.";
    target.dataset.loadError = "true";
  }
});

document.querySelectorAll("[data-year]").forEach((element) => {
  element.textContent = String(new Date().getFullYear());
});
