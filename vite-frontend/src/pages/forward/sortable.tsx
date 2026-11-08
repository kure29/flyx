import {
  DndContext,
  pointerWithin,
  DragEndEvent,
  type DraggableAttributes,
  type DraggableSyntheticListeners,
  type SensorDescriptor,
  type SensorOptions,
} from "@dnd-kit/core";
import { SortableContext, rectSortingStrategy } from "@dnd-kit/sortable";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ChevronDown, GripVertical } from "lucide-react";

import { ListGroup, type RowStatus } from "@/components/page-kit";
import { AnimatedPage } from "@/components/animated-page";
import { Forward, ForwardTunnelGroup } from "@/pages/forward/types";
import { buildTunnelGroupSortableId } from "@/pages/forward/grouping";

/** Drag-and-drop rows and groups, and the page shell, of the forward page. */
export const DragHandle = ({
  attributes,
  listeners,
  title,
  reveal = "sm:opacity-0 sm:group-hover:opacity-100",
}: {
  attributes?: DraggableAttributes;
  listeners?: DraggableSyntheticListeners;
  title: string;
  /** Classes that hide the handle until its row (or header) is hovered. */
  reveal?: string;
}) => (
  <span
    className={`inline-flex cursor-grab touch-none p-1 text-default-300 transition-opacity hover:text-default-500 active:cursor-grabbing ${reveal}`}
    title={title}
    {...attributes}
    {...listeners}
  >
    <GripVertical className="h-4 w-4" />
  </span>
);

export const buildSortableStyle = (
  transform: ReturnType<typeof useSortable>["transform"],
  transition: string | undefined,
  isDragging: boolean,
): React.CSSProperties => ({
  transform: transform
    ? CSS.Transform.toString({
        ...transform,
        x: Math.round(transform.x),
        y: Math.round(transform.y),
      })
    : undefined,
  transition: isDragging ? undefined : transition || undefined,
  opacity: isDragging ? 0.55 : 1,
  willChange: isDragging ? "transform" : undefined,
  zIndex: isDragging ? 1 : undefined,
  position: isDragging ? "relative" : undefined,
});

// 可拖拽的隧道分组
export const SortableTunnelGroup = ({
  groupUserId,
  tunnel,
  collapsed,
  onToggleCollapsed,
  sensors,
  onDragEnd,
  children,
}: {
  groupUserId: number;
  tunnel: ForwardTunnelGroup;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  sensors: SensorDescriptor<SensorOptions>[];
  onDragEnd: (event: DragEndEvent) => void;
  children: React.ReactNode;
}) => {
  const sortableId = buildTunnelGroupSortableId(groupUserId, tunnel.tunnelKey);
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: sortableId });

  return (
    <div
      ref={setNodeRef}
      style={buildSortableStyle(transform, transition, isDragging)}
    >
      {/* 组内规则单独排序；放在 ListGroup 外，免得 DndContext 的辅助节点落进网格 */}
      <DndContext
        collisionDetection={pointerWithin}
        sensors={sensors}
        onDragEnd={onDragEnd}
      >
        <SortableContext
          items={tunnel.items.map((item) => item.id).filter((id) => id > 0)}
          strategy={rectSortingStrategy}
        >
          <ListGroup
            title={
              <span className="group/head flex min-w-0 flex-1 items-center gap-2">
                <button
                  aria-expanded={!collapsed}
                  className="flex min-w-0 items-center gap-1 text-left"
                  title={collapsed ? "展开分组" : "折叠分组"}
                  type="button"
                  onClick={onToggleCollapsed}
                >
                  <ChevronDown
                    className={`h-4 w-4 shrink-0 text-default-400 transition-transform ${
                      collapsed ? "-rotate-90" : ""
                    }`}
                  />
                  <span className="truncate">{tunnel.tunnelName}</span>
                </button>
                <span className="shrink-0 text-xs font-normal text-default-400">
                  {tunnel.items.length}
                </span>
                <span className="-my-1 ml-auto flex shrink-0">
                  <DragHandle
                    attributes={attributes}
                    listeners={listeners}
                    reveal="sm:opacity-0 sm:group-hover/head:opacity-100"
                    title="拖动分组排序"
                  />
                </span>
              </span>
            }
          >
            {collapsed ? null : children}
          </ListGroup>
        </SortableContext>
      </DndContext>
    </div>
  );
};

// 可拖拽的规则行
export const SortableForwardRow = ({
  id,
  children,
}: {
  id: number;
  children: (handle: React.ReactNode) => React.ReactNode;
}) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  return (
    <div
      ref={setNodeRef}
      className="list-row-wrap"
      style={buildSortableStyle(transform, transition, isDragging)}
    >
      {children(
        <DragHandle
          attributes={attributes}
          listeners={listeners}
          title="拖动排序"
        />,
      )}
    </div>
  );
};

export const getForwardRowStatus = (status: number): RowStatus => {
  switch (status) {
    case 1:
      return "ok";
    case 0:
      return "off";
    case -1:
      return "bad";
    default:
      return "warn";
  }
};

export const ForwardPageShell = ({
  embedded,
  children,
}: {
  embedded: boolean;
  children: React.ReactNode;
}) =>
  embedded ? (
    <div>{children}</div>
  ) : (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      {children}
    </AnimatedPage>
  );

export const getForwardDisplayFlow = (forward: Forward): number => {
  const directFlow = (forward.inFlow || 0) + (forward.outFlow || 0);

  if (directFlow > 0) {
    return directFlow;
  }

  return forward.federationShareFlow || 0;
};
