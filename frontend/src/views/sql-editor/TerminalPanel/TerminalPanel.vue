<template>
  <div
    class="flex h-full w-full flex-col justify-start items-start overflow-hidden"
  >
    <EditorAction @execute="handleExecute" />

    <ConnectionPathBar />

    <div
      v-if="!tabStore.isDisconnected"
      ref="queryListContainerRef"
      class="w-full flex-1 overflow-y-auto bg-dark-bg"
    >
      <div
        ref="queryListRef"
        class="w-full flex flex-col"
        :data-height="queryListHeight"
      >
        <div v-for="(query, i) in queryList" :key="i" class="relative">
          <CompactSQLEditor
            v-model:sql="query.sql"
            class="min-h-[2rem]"
            :readonly="!isEditableQueryItem(query)"
            @execute="handleExecute"
          />
          <div v-if="query.queryResult" class="max-h-[20rem] overflow-y-auto">
            <TableView
              :query-result="query.queryResult.data"
              :loading="query.status === 'RUNNING'"
              :dark="true"
            />
          </div>
          <div
            v-else-if="query.status === 'CANCELLED'"
            class="text-control-light pl-2"
          >
            {{ $t("common.cancelled") }}
          </div>

          <div
            v-if="query.status === 'RUNNING'"
            class="absolute inset-0 bg-black/20 flex justify-center items-center gap-2"
          >
            <BBSpin />
            <div
              v-if="query === currentQuery && expired"
              class="text-gray-400 cursor-pointer hover:underline select-none"
              @click="handleCancelQuery"
            >
              {{ $t("common.cancel") }}
            </div>
          </div>
        </div>
      </div>
    </div>
    <ConnectionHolder v-else />
  </div>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from "vue";
import { useElementSize } from "@vueuse/core";

import { ExecuteConfig, ExecuteOption, WebTerminalQueryItem } from "@/types";
import { useTabStore, useWebTerminalStore } from "@/store";
import CompactSQLEditor from "./CompactSQLEditor.vue";
import {
  EditorAction,
  ConnectionPathBar,
  ConnectionHolder,
  TableView,
} from "../EditorCommon";
import { useExecuteSQL } from "@/composables/useExecuteSQL";
import { useCancelableTimeout } from "@/composables/useCancelableTimeout";

const QUERY_TIMEOUT_MS = 5000;

const tabStore = useTabStore();
const webTerminalStore = useWebTerminalStore();

const queryList = computed(() => {
  return webTerminalStore.getQueryListByTab(tabStore.currentTab);
});

const queryListContainerRef = ref<HTMLDivElement>();
const queryListRef = ref<HTMLDivElement>();

const currentQuery = computed(
  () => queryList.value[queryList.value.length - 1]
);

const { execute } = useExecuteSQL();

const queryTimer = useCancelableTimeout(QUERY_TIMEOUT_MS);
const { expired } = queryTimer;

const isEditableQueryItem = (query: WebTerminalQueryItem): boolean => {
  return query === currentQuery.value && query.status === "IDLE";
};

const handleExecute = async (
  query: string,
  config: ExecuteConfig,
  option?: ExecuteOption
) => {
  const queryItem = currentQuery.value;
  if (queryItem.status !== "IDLE") {
    return;
  }

  // Prevent executing empty query;
  if (!query) {
    return;
  }

  try {
    queryTimer.start();
    queryItem.executeParams = { query, config, option };
    queryItem.status = "RUNNING";

    const sqlResultSet = await execute(query, config, option);

    // If the queryItem is still the currentQuery
    // which means it hasn't been cancelled.
    if (queryItem === currentQuery.value) {
      queryItem.queryResult = sqlResultSet;
      queryList.value.push({
        sql: "",
        status: "IDLE",
      });
      // Clear the tab's statement and keep it sync with the latest query
      tabStore.currentTab.statement = "";
      tabStore.currentTab.selectedStatement = "";
    }
  } finally {
    queryTimer.stop();
    if (queryItem.status === "RUNNING") {
      // The query is still not cancelled
      queryItem.status = "FINISHED";
    }
  }
};

const handleCancelQuery = async () => {
  queryTimer.stop();
  currentQuery.value.status = "CANCELLED";
  queryList.value.push({
    sql: "",
    status: "IDLE",
  });
  // Clear the tab's statement and keep it sync with the latest query
  tabStore.currentTab.statement = "";
  tabStore.currentTab.selectedStatement = "";
};

const { height: queryListHeight } = useElementSize(queryListRef);

watch(queryListHeight, () => {
  // Always scroll to the bottom as if we are in a real terminal.
  requestAnimationFrame(() => {
    const container = queryListContainerRef.value;
    if (container) {
      container.scrollTo(0, container.scrollHeight);
    }
  });
});
</script>
