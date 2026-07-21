<script>
  import { onMount } from 'svelte';
  import { apiFetch } from '../lib/api.js';
  import { assignCompositionIds, autoUnwrap, removeNodeFromComposition, findNodeInComposition } from '../lib/treeUtils.js';
  import CompositionNode from './CompositionNode.svelte';

  let {
    node = $bindable(),
    allVMs = [],
  } = $props();

  let tree = $state(null);
  let dragSourceId = $state(null);
  let dropTargetId = $state(null);
  let dropIsOperation = $state(false);
  let availableVMs = $state([]);

  function getDepth(childId) {
    const info = findNodeInComposition(tree, childId);
    if (!info) return 0;
    let depth = 0;
    let current = info.parent;
    while (current) {
      depth++;
      current = findNodeInComposition(tree, current.__id)?.parent;
    }
    return depth;
  }

  function normalizeFromAPI(node) {
    if (!node || typeof node !== 'object') return node;
    if (node.collection !== undefined && node.vm === undefined) {
      node.vm = node.collection;
      delete node.collection;
    }
    if (Array.isArray(node.sources)) {
      node.sources = node.sources.map(normalizeFromAPI);
    }
    return node;
  }

  function autoExpandPopulated(node) {
    if (!node || typeof node !== 'object') return;
    if (!node.operation) {
      if (node.filter_expr && (node.filter_expr.key || (node.filter_expr.and?.length) || (node.filter_expr.or?.length) || node.filter_expr.not)) {
        node._expandedFilter = true;
      }
      if (node.sort_expr && node.sort_expr.length > 0) {
        node._expandedSort = true;
      }
    }
    if (Array.isArray(node.sources)) {
      for (const child of node.sources) autoExpandPopulated(child);
    }
  }

  function initTree(source) {
    if (!source) { tree = null; return; }
    const copy = normalizeFromAPI({ ...source });
    tree = autoUnwrap(copy);
    autoExpandPopulated(tree);
    assignCompositionIds(tree);
  }

  let lastInitSource = null;
  let internalUpdate = false;
  $effect(() => {
    if (internalUpdate) { internalUpdate = false; return; }
    if (node === lastInitSource) return;
    lastInitSource = node;
    initTree(node);
  });

  function emitChange() {
    internalUpdate = true;
    node = tree;
  }

  onMount(() => {
    if (allVMs.length > 0) {
      availableVMs = allVMs;
    } else {
      apiFetch('/api/v1/virtual-models').then(vms => {
        availableVMs = vms;
      }).catch(() => {});
    }
  });

  function compactOperations(n) {
    if (!n || typeof n !== 'object') return n;
    if (Array.isArray(n.sources)) {
      const compacted = n.sources.map(compactOperations).filter(Boolean);
      if (compacted.length === 0) return null;
      if (compacted.length === 1) return compacted[0];
      return { ...n, sources: compacted };
    }
    return n;
  }

  function addToOperation(operationNode, childNode) {
    if (!operationNode.sources) operationNode.sources = [];
    operationNode.sources.push(childNode);
    operationNode._expanded = true;
  }

  function wrapInOperation(existing, newNode) {
    return {
      operation: 'union',
      sources: [existing, newNode],
      _expanded: true
    };
  }

  function addFilterSource(parentId = null) {
    const newNode = { vm: '' };
    if (parentId) {
      const info = findNodeInComposition(tree, parentId);
      if (info?.node?.operation) {
        const newSources = [...(info.node.sources || []), newNode];
        const newOp = { ...info.node, sources: newSources, _expanded: true };
        tree = replaceAlongPath(tree, parentId, newOp);
        assignCompositionIds(tree);
        emitChange();
      }
      return;
    }
    if (!tree) {
      tree = newNode;
    } else {
      tree = wrapInOperation(tree, newNode);
    }
    assignCompositionIds(tree);
    emitChange();
  }

  function addOperation(parentId = null) {
    const newOp = { operation: 'union', sources: [], _expanded: true };
    if (parentId) {
      const info = findNodeInComposition(tree, parentId);
      if (info?.node?.operation) {
        const newSources = [...(info.node.sources || []), newOp];
        const updatedOp = { ...info.node, sources: newSources, _expanded: true };
        tree = replaceAlongPath(tree, parentId, updatedOp);
        assignCompositionIds(tree);
        emitChange();
      }
      return;
    }
    if (!tree) {
      tree = newOp;
    } else {
      tree = wrapInOperation(tree, newOp);
    }
    assignCompositionIds(tree);
    emitChange();
  }

  function removeItem(id) {
    if (!tree) return;
    if (!tree.sources) {
      if (tree.__id === id) {
        tree = null;
        emitChange();
      }
      return;
    }
    const newTree = removeNodeFromComposition(tree, id);
    if (!newTree) { tree = null; emitChange(); return; }
    tree = compactOperations(newTree);
    assignCompositionIds(tree);
    emitChange();
  }

  function setItemVM(id, vmName) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node.vm = vmName;
      emitChange();
    }
  }

  function setItemOperation(id, op) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node.operation = op;
      emitChange();
    }
  }

  function toggleItemExpanded(id) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node._expanded = !info.node._expanded;
      emitChange();
    }
  }

  function toggleItemFilter(id) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node._expandedFilter = !info.node._expandedFilter;
      emitChange();
    }
  }

  function toggleItemSort(id) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      info.node._expandedSort = !info.node._expandedSort;
      emitChange();
    }
  }

  function setItemFilter(id, filterExpr) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      if (filterExpr && (filterExpr.and || filterExpr.or || filterExpr.key)) {
        info.node.filter_expr = filterExpr;
      } else {
        delete info.node.filter_expr;
      }
      emitChange();
    }
  }

  function setItemSort(id, sortCriteria) {
    const info = findNodeInComposition(tree, id);
    if (info?.node) {
      if (sortCriteria && sortCriteria.length > 0) {
        info.node.sort_expr = sortCriteria;
      } else {
        delete info.node.sort_expr;
      }
      emitChange();
    }
  }

  function cleanNode(node) {
    if (!node || typeof node !== 'object') return null;
    let cleaned = { ...node };
    if (Array.isArray(cleaned.sources)) {
      cleaned.sources = cleaned.sources.map(cleanNode).filter(Boolean);
      if (cleaned.operation) {
        if (cleaned.sources.length === 0) return null;
        if (cleaned.sources.length === 1) cleaned = cleaned.sources[0];
      }
    }
    delete cleaned.__id;
    delete cleaned._expanded;
    delete cleaned._expandedFilter;
    delete cleaned._expandedSort;
    if (cleaned.vm !== undefined) {
      cleaned.collection = cleaned.vm;
      delete cleaned.vm;
    }
    if (cleaned.filter_expr && typeof cleaned.filter_expr === 'object') {
      const fe = cleaned.filter_expr;
      const hasContent = fe.key || (Array.isArray(fe.and) && fe.and.length > 0) || (Array.isArray(fe.or) && fe.or.length > 0) || fe.not;
      if (!hasContent) delete cleaned.filter_expr;
    }
    if (cleaned.sort_expr && Array.isArray(cleaned.sort_expr) && cleaned.sort_expr.length === 0) {
      delete cleaned.sort_expr;
    }
    if (cleaned.collection === '') delete cleaned.collection;
    if (!cleaned.operation && cleaned.collection === undefined && !cleaned.filter_expr && !(Array.isArray(cleaned.sort_expr) && cleaned.sort_expr.length > 0) && !(Array.isArray(cleaned.sources) && cleaned.sources.length > 0)) return null;
    return cleaned;
  }

  export function getComposition() {
    if (!tree) return null;
    return cleanNode(tree);
  }

  // ─── Native HTML5 Drag & Drop ──────────────────────────────────────────

  function handleDragStart(e, id) {
    dragSourceId = id;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', id);
  }

  function handleDragOver(e, id, isOpTarget = false) {
    if (id === dragSourceId) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    dropTargetId = id;
    dropIsOperation = isOpTarget;
  }

  function handleDragLeave(e) {
    const related = e.relatedTarget;
    if (related && e.currentTarget.contains(related)) return;
    dropTargetId = null;
    dropIsOperation = false;
  }

  function handleDrop(e, targetId, isOpTarget = false) {
    e.preventDefault();
    dropTargetId = null;
    dropIsOperation = false;

    const sourceId = dragSourceId;
    dragSourceId = null;

    if (!sourceId || sourceId === targetId) return;

    const sourceInfo = findNodeInComposition(tree, sourceId);
    if (!sourceInfo?.node) return;
    const sourceNode = { ...sourceInfo.node };

    let check = targetId;
    while (check) {
      if (check === sourceId) return;
      const ci = findNodeInComposition(tree, check);
      check = ci?.parent?.__id ?? null;
    }

    const newTree = removeNodeFromComposition(tree, sourceId);
    if (!newTree) return;

    if (isOpTarget) {
      const targetInfo = findNodeInComposition(newTree, targetId);
      if (!targetInfo?.node) return;
      if (!targetInfo.node.sources) targetInfo.node.sources = [];
      const newTarget = { ...targetInfo.node, sources: [...targetInfo.node.sources, sourceNode], _expanded: true };
      tree = replaceAlongPath(newTree, targetId, newTarget);
    } else {
      const targetInfo = findNodeInComposition(newTree, targetId);
      if (!targetInfo?.node) return;
      const targetParentId = targetInfo.parent?.__id ?? null;

      if (targetParentId === null) {
        tree = wrapInOperation(newTree, sourceNode);
      } else {
        const parentInfo = findNodeInComposition(newTree, targetParentId);
        if (!parentInfo?.node?.sources) return;
        const idx = parentInfo.node.sources.findIndex(s => s.__id === targetId);
        if (idx === -1) return;
        const newParentSources = [...parentInfo.node.sources];
        newParentSources.splice(idx, 0, sourceNode);
        const newParent = { ...parentInfo.node, sources: newParentSources };
        tree = replaceAlongPath(newTree, targetParentId, newParent);
      }
    }

    tree = compactOperations(tree);
    assignCompositionIds(tree);
    emitChange();
  }

  function replaceAlongPath(treeRoot, replaceId, newNode) {
    if (treeRoot.__id === replaceId) return newNode;
    if (!Array.isArray(treeRoot.sources)) return treeRoot;
    let found = false;
    const newSources = treeRoot.sources.map(child => {
      if (child.__id === replaceId) {
        found = true;
        return newNode;
      }
      const replaced = replaceAlongPath(child, replaceId, newNode);
      if (replaced !== child) found = true;
      return replaced;
    });
    if (!found) return treeRoot;
    return { ...treeRoot, sources: newSources };
  }

  function handleDragEnd() {
    dragSourceId = null;
    dropTargetId = null;
    dropIsOperation = false;
  }

  function handleCanvasDragOver(e) {
    if (!dropTargetId) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    }
  }

  function handleCanvasDrop(e) {
    if (!dropTargetId && dragSourceId) {
      e.preventDefault();
      const sourceInfo = findNodeInComposition(tree, dragSourceId);
      if (sourceInfo?.parent) {
        const sourceNode = { ...sourceInfo.node };
        const newTree = removeNodeFromComposition(tree, dragSourceId);
        if (newTree) {
          tree = wrapInOperation(compactOperations(newTree), sourceNode);
          assignCompositionIds(tree);
          emitChange();
        }
      }
    }
    dropTargetId = null;
    dragSourceId = null;
    dropIsOperation = false;
  }
</script>

<div class="space-y-1"
  role="list"
  ondragover={handleCanvasDragOver}
  ondrop={handleCanvasDrop}
>
  {#if tree}
    <CompositionNode
      node={tree}
      depth={0}
      {availableVMs}
      {dragSourceId}
      {dropTargetId}
      {dropIsOperation}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      onDragEnd={handleDragEnd}
      onToggleExpanded={toggleItemExpanded}
      onToggleFilter={toggleItemFilter}
      onToggleSort={toggleItemSort}
      onSetVM={setItemVM}
      onSetOperation={setItemOperation}
      onSetFilter={setItemFilter}
      onSetSort={setItemSort}
      onRemove={removeItem}
      onAddFilterSource={addFilterSource}
      onAddOperation={addOperation}
    />
  {/if}

  <div class="flex gap-2 mt-3" style="padding-left: 8px">
    <button
      type="button"
      class="text-xs text-amber-500 hover:text-amber-400 border border-gray-700 rounded px-2 py-1 hover:border-amber-600"
      onclick={() => addFilterSource()}
    >+ Source</button>
    <button
      type="button"
      class="text-xs text-blue-400 hover:text-blue-300 border border-gray-700 rounded px-2 py-1 hover:border-blue-500"
      onclick={() => addOperation()}
    >+ Op</button>
  </div>
</div>
