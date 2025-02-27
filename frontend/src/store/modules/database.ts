import { defineStore } from "pinia";
import axios from "axios";
import { computed, unref, watch, markRaw } from "vue";
import {
  Database,
  DatabaseFind,
  DatabaseId,
  DatabaseLabel,
  DatabaseState,
  DataSource,
  empty,
  EMPTY_ID,
  EnvironmentId,
  Instance,
  InstanceId,
  MaybeRef,
  PrincipalId,
  Project,
  ProjectId,
  ResourceIdentifier,
  ResourceObject,
  unknown,
  UNKNOWN_ID,
} from "@/types";
import { useDataSourceStore } from "./dataSource";
import { useInstanceStore } from "./instance";
import { useProjectStore } from "./project";

function convert(
  database: ResourceObject,
  includedList: ResourceObject[]
): Database {
  // We first populate the id for instance, project and dataSourceList.
  // And if we also provide the detail info for those objects in the includedList,
  // then we convert them to the detailed objects.
  const instanceId = (
    database.relationships!.instance.data as ResourceIdentifier
  ).id;
  let instance: Instance = unknown("INSTANCE") as Instance;
  instance.id = parseInt(instanceId);

  const projectId = (database.relationships!.project.data as ResourceIdentifier)
    .id;
  let project: Project = unknown("PROJECT") as Project;
  project.id = parseInt(projectId);

  const dataSourceIdList = database.relationships!.dataSource
    .data as ResourceIdentifier[];
  const dataSourceList: DataSource[] = [];
  for (const item of dataSourceIdList) {
    const dataSource = unknown("DATA_SOURCE") as DataSource;
    dataSource.id = parseInt(item.id);
    dataSourceList.push(dataSource);
  }

  const instanceStore = useInstanceStore();
  const projectStore = useProjectStore();
  for (const item of includedList || []) {
    if (item.type == "instance" && item.id == instanceId) {
      instance = instanceStore.convert(item, includedList);
    }
    if (item.type == "project" && item.id == projectId) {
      project = projectStore.convert(item, includedList);
    }
  }

  const labels: DatabaseLabel[] = [];
  try {
    const array = JSON.parse(database.attributes.labels as any);
    if (Array.isArray(array)) {
      array.forEach((item) => {
        if (
          item &&
          typeof item["key"] === "string" &&
          typeof item["value"] === "string"
        ) {
          labels.push(item);
        }
      });
    }
  } catch {
    // nothing to catch
  }

  // Only able to assign an empty data source list / anomaly list, otherwise would cause circular dependency.
  // This should be fine as e.g. we shouldn't access data source via dataSource.database.dataSourceList
  const databaseWPartial = {
    ...(database.attributes as Omit<
      Database,
      "id" | "instance" | "project" | "dataSourceList" | "labels"
    >),
    id: parseInt(database.id),
    instance,
    project,
    labels,
    dataSourceList: [],
  };

  for (const item of includedList || []) {
    if (
      item.type == "data-source" &&
      item.attributes.databaseId == database.id
    ) {
      const i = dataSourceList.findIndex(
        (dataSource: DataSource) => parseInt(item.id) == dataSource.id
      );
      if (i != -1) {
        dataSourceList[i] = useDataSourceStore().convert(item);
        dataSourceList[i].instanceId = instance.id;
        dataSourceList[i].databaseId = databaseWPartial.id;
      }
    }
  }

  return markRaw({
    ...(databaseWPartial as Omit<Database, "dataSourceList">),
    dataSourceList,
  });
}

const databaseSorter = (a: Database, b: Database): number => {
  let result = a.instance.name.localeCompare(b.instance.name);
  if (result != 0) {
    return result;
  }

  result = a.instance.environment.name.localeCompare(
    b.instance.environment.name
  );
  if (result != 0) {
    return result;
  }

  result = a.project.name.localeCompare(b.project.name);
  if (result != 0) {
    return result;
  }

  return a.name.localeCompare(b.name);
};

export const useDatabaseStore = defineStore("database", {
  state: (): DatabaseState => ({
    databaseListByInstanceId: new Map(),
    databaseListByProjectId: new Map(),
  }),
  actions: {
    convert(
      database: ResourceObject,
      includedList: ResourceObject[]
    ): Database {
      return convert(database, includedList);
    },
    getDatabaseListByInstanceId(instanceId: InstanceId): Database[] {
      return this.databaseListByInstanceId.get(instanceId) || [];
    },
    getDatabaseListByPrincipalId(userId: PrincipalId): Database[] {
      const list: Database[] = [];
      for (const [_, databaseList] of this.databaseListByInstanceId) {
        databaseList.forEach((item: Database) => {
          for (const member of item.project.memberList) {
            if (member.principal.id == userId) {
              list.push(item);
              break;
            }
          }
        });
      }
      return list;
    },
    getDatabaseListByEnvironmentId(environmentId: EnvironmentId): Database[] {
      const list: Database[] = [];
      for (const [_, databaseList] of this.databaseListByInstanceId) {
        databaseList.forEach((item: Database) => {
          if (item.instance.environment.id == environmentId) {
            list.push(item);
          }
        });
      }
      return list;
    },
    getDatabaseListByProjectId(projectId: ProjectId): Database[] {
      return this.databaseListByProjectId.get(projectId) || [];
    },
    getDatabaseById(databaseId: DatabaseId, instanceId?: InstanceId): Database {
      if (databaseId == EMPTY_ID) {
        return empty("DATABASE") as Database;
      }

      if (instanceId) {
        const list = this.databaseListByInstanceId.get(instanceId) || [];
        return (
          list.find((item) => item.id == databaseId) ||
          (unknown("DATABASE") as Database)
        );
      }

      for (const [_, list] of this.databaseListByInstanceId) {
        const database = list.find((item) => item.id == databaseId);
        if (database) {
          return database;
        }
      }

      return unknown("DATABASE") as Database;
    },
    setDatabaseListByProjectId({
      databaseList,
      projectId,
    }: {
      databaseList: Database[];
      projectId: ProjectId;
    }) {
      this.databaseListByProjectId.set(projectId, databaseList);
    },
    upsertDatabaseList({
      databaseList,
      instanceId,
    }: {
      databaseList: Database[];
      instanceId?: InstanceId;
    }) {
      if (instanceId) {
        this.databaseListByInstanceId.set(instanceId, databaseList);
      } else {
        for (const database of databaseList) {
          const listByInstance = this.databaseListByInstanceId.get(
            database.instance.id
          );
          if (listByInstance) {
            const i = listByInstance.findIndex(
              (item: Database) => item.id == database.id
            );
            if (i != -1) {
              listByInstance[i] = database;
            } else {
              listByInstance.push(database);
            }
          } else {
            this.databaseListByInstanceId.set(database.instance.id, [database]);
          }

          const listByProject = this.databaseListByProjectId.get(
            database.project.id
          );
          if (listByProject) {
            const i = listByProject.findIndex(
              (item: Database) => item.id == database.id
            );
            if (i != -1) {
              listByProject[i] = database;
            } else {
              listByProject.push(database);
            }
          } else {
            this.databaseListByProjectId.set(database.project.id, [database]);
          }
        }
      }
    },
    async fetchDatabaseList(databaseFind?: DatabaseFind) {
      const queryList = [];
      if (databaseFind?.projectId) {
        queryList.push(`project=${databaseFind.projectId}`);
      }
      if (databaseFind?.instanceId) {
        queryList.push(`instance=${databaseFind.instanceId}`);
      }
      if (databaseFind?.name) {
        queryList.push(`name=${databaseFind.name}`);
      }
      if (databaseFind?.syncStatus) {
        queryList.push(`syncStatus=${databaseFind.syncStatus}`);
      }
      const data = (await axios.get(`/api/database?${queryList.join("&")}`))
        .data;
      const databaseList: Database[] = data.data.map(
        (database: ResourceObject) => {
          return convert(database, data.included);
        }
      );
      databaseList.sort(databaseSorter);

      this.upsertDatabaseList({ databaseList });

      return databaseList;
    },
    async fetchDatabaseListByInstanceId(instanceId: InstanceId) {
      const databaseList = await this.fetchDatabaseList({
        instanceId,
      });

      return databaseList;
    },
    async fetchDatabaseByInstanceIdAndName({
      instanceId,
      name,
    }: {
      instanceId: InstanceId;
      name: string;
    }) {
      const databaseList = await this.fetchDatabaseList({
        instanceId,
        name,
      });

      return databaseList[0];
    },
    async fetchDatabaseListByProjectId(projectId: ProjectId) {
      const databaseList = await this.fetchDatabaseList({
        projectId,
      });

      this.setDatabaseListByProjectId({ databaseList, projectId });

      return databaseList;
    },
    async fetchDatabaseListByEnvironmentId(environmentId: EnvironmentId) {
      // Don't fetch the data source info as the current user may not have access to the
      // database of this particular environment.
      const data = (
        await axios.get(`/api/database?environment=${environmentId}`)
      ).data;
      const databaseList: Database[] = data.data.map(
        (database: ResourceObject) => {
          return convert(database, data.included);
        }
      );
      databaseList.sort(databaseSorter);

      this.upsertDatabaseList({ databaseList });

      return databaseList;
    },
    async fetchDatabaseById(databaseId: DatabaseId) {
      const url = `/api/database/${databaseId}`;
      const data = (await axios.get(url)).data;
      const database = convert(data.data, data.included);

      this.upsertDatabaseList({
        databaseList: [database],
      });

      return database;
    },
    async getOrFetchDatabaseById(databaseId: DatabaseId) {
      const storedDatabase = this.getDatabaseById(databaseId);
      if (storedDatabase.id !== UNKNOWN_ID) {
        return storedDatabase;
      }
      return this.fetchDatabaseById(databaseId);
    },
    async fetchDatabaseSchemaById(databaseId: DatabaseId): Promise<string> {
      const url = `/api/database/${databaseId}/schema`;
      const schema = (await axios.get(url)).data as string;
      return schema;
    },
    async transferProject({
      databaseId,
      projectId,
      labels,
    }: {
      databaseId: DatabaseId;
      projectId: ProjectId;
      labels?: DatabaseLabel[];
    }) {
      const attributes: any = { projectId };
      if (labels) {
        attributes.labels = JSON.stringify(labels);
      }
      const data = (
        await axios.patch(`/api/database/${databaseId}`, {
          data: {
            type: "databasePatch",
            attributes,
          },
        })
      ).data;

      const updatedDatabase = convert(data.data, data.included);

      this.upsertDatabaseList({
        databaseList: [updatedDatabase],
      });

      return updatedDatabase;
    },
    async patchDatabaseLabels({
      databaseId,
      labels,
    }: {
      databaseId: DatabaseId;
      labels: DatabaseLabel[];
    }) {
      const data = (
        await axios.patch(`/api/database/${databaseId}`, {
          data: {
            type: "databasePatch",
            attributes: {
              labels: JSON.stringify(labels),
            },
          },
        })
      ).data;
      const updatedDatabase = convert(data.data, data.included);

      this.upsertDatabaseList({
        databaseList: [updatedDatabase],
      });

      return updatedDatabase;
    },
  },
});

export const useDatabaseById = (databaseId: MaybeRef<DatabaseId>) => {
  const store = useDatabaseStore();
  watch(
    () => unref(databaseId),
    (id) => {
      if (id !== UNKNOWN_ID) {
        if (store.getDatabaseById(id).id === UNKNOWN_ID) {
          store.fetchDatabaseById(id);
        }
      }
    },
    { immediate: true }
  );

  return computed(() => store.getDatabaseById(unref(databaseId)));
};
