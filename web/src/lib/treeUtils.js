/**
 * Immutable tree utilities for condition/sort tree drag-and-drop.
 * All functions are pure — no mutation, spread-based cloning.
 */

let _idCounter = 0;

/**
 * Assign stable __id to every node via DFS.
 * Mutates the input tree by adding __id fields (one-time call, acceptable).
 * @param {object} node - tree node
 * @param {string} [parentId] - parent's assigned id
 * @returns {object} same node reference with __id set
 */
export function assignStableIds(node, parentId) {
  if (!node || typeof node !== 'object') return node;

  if (!node.__id) {
    node.__id = `node-${Date.now()}-${_idCounter++}`;
  }

  for (const key of ['and', 'or', 'not', 'children']) {
    if (Array.isArray(node[key])) {
      for (const child of node[key]) {
        assignStableIds(child, node.__id);
      }
    }
    // 'not' wraps a single object, not array
    if (key === 'not' && node[key] && typeof node[key] === 'object' && !Array.isArray(node[key])) {
      assignStableIds(node[key], node.__id);
    }
  }

  return node;
}

/**
 * Find a node and its parent info by __id.
 * @param {object} tree - root node
 * @param {string} id - __id to find
 * @returns {{ node: object, parent: object|null, index: number, parentKey: string }|null}
 */
export function findNodeAndParent(tree, id) {
  if (!tree || !id) return null;

  function walk(node, parent, parentKey, index) {
    if (node.__id === id) {
      return { node, parent, index, parentKey };
    }

    // Check 'and' array
    if (Array.isArray(node.and)) {
      for (let i = 0; i < node.and.length; i++) {
        const result = walk(node.and[i], node, 'and', i);
        if (result) return result;
      }
    }

    // Check 'or' array
    if (Array.isArray(node.or)) {
      for (let i = 0; i < node.or.length; i++) {
        const result = walk(node.or[i], node, 'or', i);
        if (result) return result;
      }
    }

    // Check 'not' (single child)
    if (node.not && typeof node.not === 'object' && !Array.isArray(node.not)) {
      const result = walk(node.not, node, 'not', 0);
      if (result) return result;
    }

    return null;
  }

  return walk(tree, null, null, -1);
}

/**
 * Check if candidateId is a descendant of ancestorId.
 * @param {object} tree - root node
 * @param {string} ancestorId
 * @param {string} candidateId
 * @returns {boolean}
 */
export function isDescendant(tree, ancestorId, candidateId) {
  if (ancestorId === candidateId) return true;

  const ancestorInfo = findNodeAndParent(tree, ancestorId);
  if (!ancestorInfo) return false;

  const node = ancestorInfo.node;

  function check(n) {
    if (n.__id === candidateId) return true;
    if (Array.isArray(n.and)) {
      for (const child of n.and) {
        if (check(child)) return true;
      }
    }
    if (Array.isArray(n.or)) {
      for (const child of n.or) {
        if (check(child)) return true;
      }
    }
    if (n.not && typeof n.not === 'object' && !Array.isArray(n.not)) {
      if (check(n.not)) return true;
    }
    return false;
  }

  return check(node);
}

/**
 * Check if a node is a group (has 'and' or 'or' children).
 * @param {object} node
 * @returns {boolean}
 */
export function isGroup(node) {
  return node && !node.key && (Array.isArray(node.and) || Array.isArray(node.or));
}

/**
 * Check if a node is a NOT wrapper.
 * @param {object} node
 * @returns {boolean}
 */
export function isNot(node) {
  return node && node.not !== undefined;
}

/**
 * Get the children array of a group node.
 * @param {object} node
 * @returns {object[]}
 */
export function getChildren(node) {
  if (!node) return [];
  if (Array.isArray(node.and)) return node.and;
  if (Array.isArray(node.or)) return node.or;
  return [];
}

/**
 * Create a new group node with given children.
 * @param {string} mode - 'and' or 'or'
 * @param {object[]} children
 * @returns {object}
 */
export function makeGroup(mode, children) {
  if (mode === 'or') return { or: children };
  return { and: children };
}

/**
 * Remove a node from tree by __id (immutable). Returns new root.
 * @param {object} tree
 * @param {string} removeId
 * @returns {object} new tree
 */
export function removeNode(tree, removeId) {
  if (!tree || tree.__id === removeId) return undefined;

  const result = { ...tree };

  for (const key of ['and', 'or']) {
    if (Array.isArray(result[key])) {
      const newChildren = result[key]
        .map(child => removeNode(child, removeId))
        .filter(child => child !== undefined);
      result[key] = newChildren;
    }
  }

  if (result.not && typeof result.not === 'object' && !Array.isArray(result.not)) {
    const newNot = removeNode(result.not, removeId);
    if (newNot === undefined) {
      delete result.not;
    } else {
      result.not = newNot;
    }
  }

  return result;
}

/**
 * Insert a node into a group's children at a given index (immutable).
 * @param {object} tree
 * @param {string} groupId - __id of the group to insert into (null = root)
 * @param {object} nodeToInsert
 * @param {number} index - insertion index
 * @returns {object} new tree
 */
export function insertNode(tree, groupId, nodeToInsert, index) {
  if (!tree) return tree;

  if (tree.__id === groupId && isGroup(tree)) {
    // Clone the group with the new child inserted
    const newGroup = { ...tree };
    if (Array.isArray(newGroup.and)) {
      const newChildren = [...newGroup.and];
      newChildren.splice(index, 0, nodeToInsert);
      newGroup.and = newChildren;
    } else if (Array.isArray(newGroup.or)) {
      const newChildren = [...newGroup.or];
      newChildren.splice(index, 0, nodeToInsert);
      newGroup.or = newChildren;
    }
    return newGroup;
  }

  // Clone this node and recurse into children
  const result = { ...tree };

  for (const key of ['and', 'or']) {
    if (Array.isArray(result[key])) {
      result[key] = result[key].map(child =>
        insertNode(child, groupId, nodeToInsert, index)
      );
    }
  }

  if (result.not && typeof result.not === 'object' && !Array.isArray(result.not)) {
    result.not = insertNode(result.not, groupId, nodeToInsert, index);
  }

  return result;
}

/**
 * Move a node from one position to another (immutable).
 * @param {object} tree
 * @param {string} sourceId - __id of node to move
 * @param {string} targetId - __id of target node
 * @param {'before'|'after'|'inside'} position
 * @returns {object|null} new tree, or null if move invalid
 */
export function moveNodeInTree(tree, sourceId, targetId, position) {
  // Find source node
  const sourceInfo = findNodeAndParent(tree, sourceId);
  if (!sourceInfo) return null;

  // Prevent moving a group onto itself
  if (sourceId === targetId) return null;

  // Cycle detection for 'inside' position
  if (position === 'inside') {
    if (isDescendant(tree, sourceId, targetId)) {
      return null; // Would create cycle
    }
  }

  const sourceNode = sourceInfo.node;

  // Step 1: Remove source from current location
  let newTree = removeNode(tree, sourceId);
  if (newTree === undefined) return null;

  // Step 2: Find target in the new tree and insert
  const targetInfo = findNodeAndParent(newTree, targetId);
  if (!targetInfo) return null;

  const targetNode = targetInfo.node;

  if (position === 'inside') {
    // Insert as last child of target group
    if (!isGroup(targetNode)) return null;
    const children = getChildren(targetNode);
    return insertNode(newTree, targetId, sourceNode, children.length);
  }

  // before/after: insert relative to target in target's parent
  if (!targetInfo.parent) {
    // Target is root — insert as sibling (shouldn't happen for root)
    return null;
  }

  const insertIndex = position === 'before'
    ? targetInfo.index
    : targetInfo.index + 1;

  return insertNode(newTree, targetInfo.parent.__id, sourceNode, insertIndex);
}

/**
 * Get a flat list of all node __ids in the tree (DFS order).
 * Useful for dnd-kit index mapping.
 * @param {object} tree
 * @returns {string[]}
 */
export function flattenIds(tree) {
  if (!tree) return [];

  const ids = [tree.__id];

  for (const key of ['and', 'or']) {
    if (Array.isArray(tree[key])) {
      for (const child of tree[key]) {
        ids.push(...flattenIds(child));
      }
    }
  }

  if (tree.not && typeof tree.not === 'object' && !Array.isArray(tree.not)) {
    ids.push(...flattenIds(tree.not));
  }

  return ids;
}

/**
 * Reset the ID counter (for testing).
 */
export function resetIdCounter() {
  _idCounter = 0;
}
