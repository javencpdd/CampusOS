<script setup lang="ts">
withDefaults(defineProps<{ name?: string }>(), { name: "arrow" });
const paths: Record<string, string[]> = {
  arrow: ["M5 12h14", "m13 6 6 6-6 6"],
  diagonal: ["M7 17 17 7", "M7 7h10v10"],
  chat: [
    "M21 11.5a8.4 8.4 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.4 8.4 0 0 1-3.8-.9L3 21l1.9-5.7A8.4 8.4 0 0 1 4 11.5a8.5 8.5 0 0 1 4.7-7.6 8.4 8.4 0 0 1 3.8-.9h.5a8.5 8.5 0 0 1 8 8v.5Z",
    "M8 10h8",
    "M8 14h5",
  ],
  file: [
    "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z",
    "M14 2v6h6",
    "M8 13h8",
    "M8 17h5",
  ],
  search: ["M21 21l-4.5-4.5", "M19 11a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z"],
  shield: ["M12 22s8-4 8-11V5l-8-3-8 3v6c0 7 8 11 8 11Z", "m8 12 3 3 5-6"],
  puzzle: [
    "M9 3H4a1 1 0 0 0-1 1v5a3 3 0 1 1 0 6v5a1 1 0 0 0 1 1h5a3 3 0 1 1 6 0h5a1 1 0 0 0 1-1v-5a3 3 0 1 1 0-6V4a1 1 0 0 0-1-1h-5a3 3 0 1 1-6 0Z",
  ],
  book: [
    "M12 7v14",
    "M3 3h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5v17h-5a4 4 0 0 0-4 1 4 4 0 0 0-4-1H3Z",
  ],
  terminal: ["m4 6 6 6-6 6", "M13 18h7"],
  layers: ["m12 3 10 6-10 6L2 9Z", "m2 15 10 6 10-6"],
};
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path
      v-for="(d, index) in paths[name] || paths.arrow"
      :key="index"
      :d="d"
    />
  </svg>
</template>
