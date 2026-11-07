# 053 List View Styling

## Objective
Update the design of Node and User list table views to match the provided screenshot:
- Use colored dots for status instead of separate "Status" text columns.
- The action buttons should have text labels (e.g. "安装", "编辑") with `variant="flat"` instead of icons.
- Add version column for Node list.
- Remove traffic columns from Node list as requested.
- Adjust User list to match this clean style.

## Tasks
- [x] Update `pages/node.tsx` list view to use the new column layout (Node name with dot, Address, Version, Actions).
- [x] Update action buttons in `pages/node.tsx` list view to use text instead of icons.
- [x] Update `pages/user.tsx` list view to use the status dot pattern.
- [x] Update action buttons in `pages/user.tsx` list view to use text instead of icons.
- [x] Ensure `selectionMode="multiple"` (or similar) is properly reflected.
