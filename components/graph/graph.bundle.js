(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.ComponentRoot = void 0;
class ComponentRoot {
    _apiCache;
    defaultDebounceOption = {
        maxWait: 1500,
        leading: false,
        trailing: true
    };
    constructor() { }
    isSamePayload(value) {
        const jsonString = JSON.stringify(value);
        if (this._apiCache === jsonString) {
            ungit.logger.debug(`ignoring redraw for same ${this.constructor.name} payload.`);
            return true;
        }
        ungit.logger.debug(`redrawing ${this.constructor.name} payload.  \n${jsonString}`);
        this._apiCache = jsonString;
        return false;
    }
    clearApiCache() {
        this._apiCache = undefined;
    }
}
exports.ComponentRoot = ComponentRoot;

},{}],2:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const Selectable = require('./selectable');
require('mina');
class Animateable extends Selectable {
    constructor(graph) {
        super(graph);
        this.element = ko.observable();
        this.previousGraph = undefined;
        this.element.subscribe((val) => {
            if (val)
                this.animate(true);
        });
        this.animate = (forceRefresh) => {
            const currentGraph = this.getGraphAttr();
            if (this.element() &&
                (forceRefresh || JSON.stringify(currentGraph) !== JSON.stringify(this.previousGraph))) {
                // dom is valid and force refresh is requested or dom moved, redraw
                if (ungit.config.isAnimate) {
                    const now = Date.now();
                    window.mina(this.previousGraph || currentGraph, currentGraph, now, now + 750, window.mina.time, (val) => {
                        this.setGraphAttr(val);
                    }, window.mina.elastic);
                }
                else {
                    this.setGraphAttr(currentGraph);
                }
                this.previousGraph = currentGraph;
            }
        };
    }
}
module.exports = Animateable;

},{"./selectable":9,"knockout":undefined,"mina":undefined}],3:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const Animateable = require('./animateable');
class EdgeViewModel extends Animateable {
    constructor(graph, nodeAsha1, nodeBsha1) {
        super(graph);
        this.nodeA = graph.getNode(nodeAsha1);
        this.nodeB = graph.getNode(nodeBsha1);
        this.getGraphAttr = ko.computed(() => {
            if (this.nodeA.isViewable() && (!this.nodeB.isViewable() || !this.nodeB.isInited)) {
                return [
                    this.nodeA.cx(),
                    this.nodeA.cy(),
                    this.nodeA.cx(),
                    this.nodeA.cy(),
                    this.nodeA.cx(),
                    graph.graphHeight(),
                    this.nodeA.cx(),
                    graph.graphHeight(),
                ];
            }
            else if (this.nodeB.isInited && this.nodeB.cx() && this.nodeB.cy()) {
                return [
                    this.nodeA.cx(),
                    this.nodeA.cy(),
                    this.nodeA.cx(),
                    this.nodeA.cy(),
                    this.nodeB.cx(),
                    this.nodeB.cy(),
                    this.nodeB.cx(),
                    this.nodeB.cy(),
                ];
            }
            else {
                return [0, 0, 0, 0, 0, 0, 0, 0];
            }
        });
        this.getGraphAttr.subscribe(this.animate.bind(this));
    }
    setGraphAttr(val) {
        this.element().setAttribute('d', `M${val.slice(0, 4).join(',')}L${val.slice(4, 8).join(',')}`);
    }
    edgeMouseOver() {
        if (this.nodeA) {
            this.nodeA.isEdgeHighlighted(true);
        }
        if (this.nodeB) {
            this.nodeB.isEdgeHighlighted(true);
        }
    }
    edgeMouseOut() {
        if (this.nodeA) {
            this.nodeA.isEdgeHighlighted(false);
        }
        if (this.nodeB) {
            this.nodeB.isEdgeHighlighted(false);
        }
    }
}
module.exports = EdgeViewModel;

},{"./animateable":2,"knockout":undefined}],4:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const octicons = require('octicons');
const components = require('ungit-components');
const programEvents = require('ungit-program-events');
const RefViewModel = require('./git-ref.js');
const HoverActions = require('./hover-actions');
const RebaseViewModel = HoverActions.RebaseViewModel;
const MergeViewModel = HoverActions.MergeViewModel;
const ResetViewModel = HoverActions.ResetViewModel;
const PushViewModel = HoverActions.PushViewModel;
const SquashViewModel = HoverActions.SquashViewModel;
class ActionBase {
    constructor(graph, text, style, icon) {
        this.graph = graph;
        this.server = graph.server;
        this.isRunning = ko.observable(false);
        this.isHighlighted = ko.computed(() => !graph.hoverGraphAction() || graph.hoverGraphAction() == this);
        this.text = text;
        this.style = style;
        this.icon = icon;
        this.cssClasses = ko.computed(() => {
            if (!this.isHighlighted() || this.isRunning()) {
                return `${this.style} dimmed`;
            }
            else {
                return this.style;
            }
        });
    }
    doPerform() {
        if (this.isRunning())
            return;
        this.graph.hoverGraphAction(null);
        this.isRunning(true);
        return this.perform()
            .catch((e) => this.server.unhandledRejection(e))
            .finally(() => {
            this.isRunning(false);
        });
    }
    dragEnter() {
        if (!this.visible())
            return;
        this.graph.hoverGraphAction(this);
    }
    dragLeave() {
        if (!this.visible())
            return;
        this.graph.hoverGraphAction(null);
    }
    mouseover() {
        this.graph.hoverGraphAction(this);
    }
    mouseout() {
        this.graph.hoverGraphAction(null);
    }
}
class Move extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Move', 'move', octicons['arrow-left'].toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return (this.graph.currentActionContext() instanceof RefViewModel &&
                this.graph.currentActionContext().node() != this.node);
        });
    }
    perform() {
        return this.graph.currentActionContext().moveTo(this.node.sha1);
    }
}
class Reset extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Reset', 'reset', octicons.trash.toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            if (!(this.graph.currentActionContext() instanceof RefViewModel))
                return false;
            const context = this.graph.currentActionContext();
            if (context.node() != this.node)
                return false;
            const remoteRef = context.getRemoteRef(this.graph.currentRemote());
            return (remoteRef &&
                remoteRef.node() &&
                context &&
                context.node() &&
                remoteRef.node() != context.node() &&
                remoteRef.node().date < context.node().date);
        });
    }
    createHoverGraphic() {
        const context = this.graph.currentActionContext();
        if (!context)
            return null;
        const remoteRef = context.getRemoteRef(this.graph.currentRemote());
        const nodes = context.node().getPathToCommonAncestor(remoteRef.node()).slice(0, -1);
        return new ResetViewModel(nodes);
    }
    perform() {
        const context = this.graph.currentActionContext();
        const remoteRef = context.getRemoteRef(this.graph.currentRemote());
        return new Promise((resolve, reject) => {
            components.showModal('yesnomodal', {
                title: 'Are you sure?',
                details: 'Resetting to ref: ' + remoteRef.name + ' cannot be undone with Ungit-Go.',
                closeFunc: async (isYes) => {
                    if (isYes) {
                        await this.server
                            .postPromise('/reset', {
                            path: this.graph.repoPath(),
                            to: remoteRef.name,
                            mode: 'hard',
                        })
                            .then(resolve)
                            .catch(reject);
                        context.node(remoteRef.node());
                    }
                    this.isRunning(false);
                },
            });
        });
    }
}
class Rebase extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Rebase', 'rebase', octicons['repo-forked'].toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return (this.graph.currentActionContext() instanceof RefViewModel &&
                (!ungit.config.showRebaseAndMergeOnlyOnRefs || this.node.refs().length > 0) &&
                this.graph.currentActionContext().current() &&
                this.graph.currentActionContext().node() != this.node);
        });
    }
    createHoverGraphic() {
        let onto = this.graph.currentActionContext();
        if (!onto)
            return;
        if (onto instanceof RefViewModel)
            onto = onto.node();
        const path = onto.getPathToCommonAncestor(this.node);
        return new RebaseViewModel(this.node, path);
    }
    perform() {
        return this.server
            .postPromise('/rebase', { path: this.graph.repoPath(), onto: this.node.sha1 })
            .catch((err) => {
            if (err.errorCode != 'merge-failed') {
                this.server.unhandledRejection(err);
            }
            else {
                ungit.logger.warn('rebase failed', err);
            }
        });
    }
}
class Merge extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Merge', 'merge', octicons['git-merge'].toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            if (!this.graph.checkedOutRef() || !this.graph.checkedOutRef().node())
                return false;
            return (this.graph.currentActionContext() instanceof RefViewModel &&
                !this.graph.currentActionContext().current() &&
                this.graph.checkedOutRef().node() == this.node);
        });
    }
    createHoverGraphic() {
        let node = this.graph.currentActionContext();
        if (!node)
            return null;
        if (node instanceof RefViewModel)
            node = node.node();
        return new MergeViewModel(this.graph, this.node, node);
    }
    perform() {
        return this.server
            .postPromise('/merge', {
            path: this.graph.repoPath(),
            with: this.graph.currentActionContext().localRefName,
        })
            .catch((err) => {
            if (err.errorCode != 'merge-failed') {
                this.server.unhandledRejection(err);
            }
            else {
                ungit.logger.warn('merge failed', err);
            }
        });
    }
}
class Push extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Push', 'push', octicons['repo-push'].toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return (this.graph.currentActionContext() instanceof RefViewModel &&
                this.graph.currentActionContext().node() == this.node &&
                this.graph.currentActionContext().canBePushed(this.graph.currentRemote()));
        });
    }
    createHoverGraphic() {
        const context = this.graph.currentActionContext();
        if (!context)
            return null;
        const remoteRef = context.getRemoteRef(this.graph.currentRemote());
        if (!remoteRef)
            return null;
        return new PushViewModel(remoteRef.node(), context.node());
    }
    perform() {
        const ref = this.graph.currentActionContext();
        const remoteRef = ref.getRemoteRef(this.graph.currentRemote());
        if (remoteRef) {
            return remoteRef.moveTo(ref.node().sha1);
        }
        else {
            return ref
                .createRemoteRef()
                .then(() => {
                if (this.graph.HEAD().name == ref.name) {
                    this.graph.HEADref().node(ref.node());
                }
            })
                .finally(() => programEvents.dispatch({ event: 'request-fetch-tags' }));
        }
    }
}
class Checkout extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Checkout', 'checkout', octicons['desktop-download'].toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            if (this.graph.currentActionContext() instanceof RefViewModel)
                return (this.graph.currentActionContext().node() == this.node &&
                    !this.graph.currentActionContext().current());
            return ungit.config.allowCheckoutNodes && this.graph.currentActionContext() == this.node;
        });
    }
    perform() {
        return this.graph.currentActionContext().checkout();
    }
}
class Delete extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Delete', 'delete', octicons.x.toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return (this.graph.currentActionContext() instanceof RefViewModel &&
                this.graph.currentActionContext().node() == this.node &&
                !this.graph.currentActionContext().current());
        });
    }
    perform() {
        const context = this.graph.currentActionContext();
        let details = `"${context.refName}"`;
        if (context.isRemoteBranch) {
            details = `<code _style="font-size: 100%">REMOTE</code> ${details}`;
        }
        details = `Deleting ${details} branch or tag cannot be undone with Ungit-Go.`;
        return new Promise((resolve, reject) => {
            components.showModal('yesnomodal', {
                title: 'Are you sure?',
                details: details,
                closeFunc: async (isYes) => {
                    if (isYes) {
                        await context.remove().then(resolve).catch(reject);
                    }
                    this.isRunning(false);
                },
            });
        });
    }
}
class CherryPick extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Cherry pick', 'cherry-pick', octicons.cpu.toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            const context = this.graph.currentActionContext();
            return context === this.node && this.graph.HEAD() && context.sha1 !== this.graph.HEAD().sha1;
        });
    }
    perform() {
        return this.server
            .postPromise('/cherrypick', { path: this.graph.repoPath(), name: this.node.sha1 })
            .catch((err) => {
            if (err.errorCode != 'merge-failed') {
                this.server.unhandledRejection(err);
            }
            else {
                ungit.logger.warn('cherrypick failed', err);
            }
        });
    }
}
class Uncommit extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Uncommit', 'uncommit', octicons.zap.toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return this.graph.currentActionContext() == this.node && this.graph.HEAD() == this.node;
        });
    }
    perform() {
        return this.server
            .postPromise('/reset', { path: this.graph.repoPath(), to: 'HEAD^', mode: 'mixed' })
            .then(() => {
            let targetNode = this.node.belowNode;
            while (targetNode && !targetNode.ancestorOfHEAD()) {
                targetNode = targetNode.belowNode;
            }
            this.graph.HEADref().node(targetNode ? targetNode : null);
            this.graph.checkedOutRef().node(targetNode ? targetNode : null);
        });
    }
}
class Revert extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Revert', 'revert', octicons.history.toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return this.graph.currentActionContext() == this.node;
        });
    }
    perform() {
        return this.server.postPromise('/revert', {
            path: this.graph.repoPath(),
            commit: this.node.sha1,
        });
    }
}
class Squash extends ActionBase {
    constructor(graph, node) {
        super(graph, 'Squash', 'squash', octicons.fold.toSVG({ height: 18 }));
        this.node = node;
        this.visible = ko.computed(() => {
            if (this.isRunning())
                return true;
            return (this.graph.currentActionContext() instanceof RefViewModel &&
                this.graph.currentActionContext().current() &&
                this.graph.currentActionContext().node() != this.node);
        });
    }
    createHoverGraphic() {
        let onto = this.graph.currentActionContext();
        if (!onto)
            return;
        if (onto instanceof RefViewModel)
            onto = onto.node();
        return new SquashViewModel(this.node, onto);
    }
    perform() {
        let onto = this.graph.currentActionContext();
        if (!onto)
            return;
        if (onto instanceof RefViewModel)
            onto = onto.node();
        // remove last element as it would be a common ancestor.
        const path = this.node.getPathToCommonAncestor(onto).slice(0, -1);
        if (path.length > 0) {
            // squashing branched out lineage
            // c is checkout with squash target of e, results in staging changes
            // from d and e on top of c
            //
            // a - b - (c)        a - b - (c) - [de]
            //  \           ->     \
            //   d  - <e>           d - <e>
            return this.server.postPromise('/squash', {
                path: this.graph.repoPath(),
                target: this.node.sha1,
            });
        }
        else {
            // squashing backward from same lineage
            // c is checkout with squash target of a, results in current ref moved
            // to a and staging changes within b and c on top of a
            //
            // <a> - b - (c)       (a) - b - c
            //                ->     \
            //                        [bc]
            return this.graph
                .currentActionContext()
                .moveTo(this.node.sha1, true)
                .then(() => this.server.postPromise('/squash', { path: this.graph.repoPath(), target: onto.sha1 }));
        }
    }
}
const GraphActions = {
    Move: Move,
    Rebase: Rebase,
    Merge: Merge,
    Push: Push,
    Reset: Reset,
    Checkout: Checkout,
    Delete: Delete,
    CherryPick: CherryPick,
    Uncommit: Uncommit,
    Revert: Revert,
    Squash: Squash,
};
module.exports = GraphActions;

},{"./git-ref.js":6,"./hover-actions":8,"knockout":undefined,"octicons":undefined,"ungit-components":undefined,"ungit-program-events":undefined}],5:[function(require,module,exports){
"use strict";
const $ = require('jquery');
const ko = require('knockout');
const components = require('ungit-components');
const programEvents = require('ungit-program-events');
const theme = require('ungit-theme');
const Animateable = require('./animateable');
const GraphActions = require('./git-graph-actions');
const maxBranchesToDisplay = parseInt((ungit.config.numRefsToShow / 5) * 3); // 3/5 of refs to show to branches
const maxTagsToDisplay = ungit.config.numRefsToShow - maxBranchesToDisplay; // 2/5 of refs to show to tags
class GitNodeViewModel extends Animateable {
    constructor(graph, sha1) {
        super(graph);
        this.graph = graph;
        this.sha1 = sha1;
        this.isInited = false;
        this.title = ko.observable();
        this.parents = ko.observableArray();
        this.commitTime = undefined; // commit time in string
        this.date = undefined; // commit time in numeric format for sort
        this.color = ko.observable();
        this.ideologicalBranch = ko.observable();
        this.remoteTags = ko.observableArray();
        this.branchesAndLocalTags = ko.observableArray();
        this.signatureDate = ko.observable();
        this.signatureMade = ko.observable();
        this.pgpVerifiedString = ko.computed(() => {
            if (this.signatureMade()) {
                return `PGP by: ${this.signatureMade()} at ${this.signatureDate()}`;
            }
        });
        this.refs = ko.computed(() => {
            const rs = this.branchesAndLocalTags().concat(this.remoteTags());
            rs.sort((a, b) => {
                if (b.current())
                    return 1;
                if (a.current())
                    return -1;
                if (a.isLocal && !b.isLocal)
                    return -1;
                if (!a.isLocal && b.isLocal)
                    return 1;
                return a.refName < b.refName ? -1 : 1;
            });
            return rs;
        });
        // These are split up like this because branches and local tags can be found in the git log,
        // whereas remote tags needs to be fetched with another command (which is much slower)
        this.branches = ko.observableArray();
        this.branchesToDisplay = ko.observableArray();
        this.tags = ko.observableArray();
        this.tagsToDisplay = ko.observableArray();
        this.refs.subscribe((newValue) => {
            if (newValue) {
                this.branches(newValue.filter((r) => r.isBranch));
                this.tags(newValue.filter((r) => r.isTag));
                this.branchesToDisplay(this.branches.slice(0, ungit.config.numRefsToShow - Math.min(this.tags().length, maxTagsToDisplay)));
                this.tagsToDisplay(this.tags.slice(0, ungit.config.numRefsToShow - this.branchesToDisplay().length));
            }
            else {
                this.branches.removeAll();
                this.tags.removeAll();
                this.branchesToDisplay.removeAll();
                this.tagsToDisplay.removeAll();
            }
        });
        this.ancestorOfHEAD = ko.observable(false);
        this.nodeIsMousehover = ko.observable(false);
        this.commitContainerVisible = ko.computed(() => this.ancestorOfHEAD() || this.nodeIsMousehover() || this.selected());
        this.isEdgeHighlighted = ko.observable(false);
        // for small empty black circle to highlight a node
        this.isNodeAccented = ko.computed(() => this.selected() || this.isEdgeHighlighted());
        // to show changed files and diff boxes on the left of node
        this.highlighted = ko.computed(() => this.nodeIsMousehover() || this.selected());
        this.selected.subscribe(() => {
            programEvents.dispatch({ event: 'graph-render' });
        });
        this.showNewRefAction = ko.computed(() => !graph.currentActionContext());
        this.showRefSearch = ko.computed(() => this.branches().length + this.tags().length > ungit.config.numRefsToShow);
        this.newBranchName = ko.observable();
        this.newBranchNameHasFocus = ko.observable(true);
        this.branchingFormVisible = ko.observable(false);
        this.canCreateRef = ko.computed(() => this.newBranchName() && this.newBranchName().trim() && !this.newBranchName().includes(' '));
        this.branchOrder = ko.observable();
        this.aboveNode = undefined;
        this.belowNode = undefined;
        this.refSearchFormVisible = ko.observable(false);
        this.commitComponent = components.create('commit', this);
        this.r = ko.observable();
        this.cx = ko.observable();
        this.cy = ko.observable();
        this.dropareaGraphActions = [
            new GraphActions.Move(this.graph, this),
            new GraphActions.Rebase(this.graph, this),
            new GraphActions.Merge(this.graph, this),
            new GraphActions.Push(this.graph, this),
            new GraphActions.Reset(this.graph, this),
            new GraphActions.Checkout(this.graph, this),
            new GraphActions.Delete(this.graph, this),
            new GraphActions.CherryPick(this.graph, this),
            new GraphActions.Uncommit(this.graph, this),
            new GraphActions.Revert(this.graph, this),
            new GraphActions.Squash(this.graph, this),
        ];
    }
    getGraphAttr() {
        return [this.cx(), this.cy()];
    }
    setGraphAttr(val) {
        this.element().setAttribute('x', val[0] - 30);
        this.element().setAttribute('y', val[1] - 30);
    }
    render() {
        this.refSearchFormVisible(false);
        if (!this.isInited)
            return;
        if (this.ancestorOfHEAD()) {
            this.r(30);
            this.cx(610);
            if (!this.aboveNode) {
                this.cy(120);
            }
            else if (this.aboveNode.ancestorOfHEAD()) {
                this.cy(this.aboveNode.cy() + 120);
            }
            else {
                this.cy(this.aboveNode.cy() + 60);
            }
        }
        else {
            this.r(15);
            this.cx(610 + 90 * this.branchOrder());
            this.cy(this.aboveNode ? this.aboveNode.cy() + 60 : 120);
        }
        if (this.aboveNode && this.aboveNode.selected()) {
            this.cy(this.aboveNode.cy() + this.aboveNode.commitComponent.element().offsetHeight + 30);
        }
        this.color(this.ideologicalBranch() ? this.ideologicalBranch().color() : theme.graphFallbackColor());
        this.animate();
    }
    setData(logEntry) {
        this.title(logEntry.message.split('\n')[0]);
        this.parents(logEntry.parents || []);
        this.commitTime = logEntry.commitDate;
        this.date = Date.parse(this.commitTime);
        this.commitComponent.setData(logEntry);
        this.signatureMade(logEntry.signatureMade);
        this.signatureDate(logEntry.signatureDate);
        (logEntry.refs || []).forEach((ref) => {
            this.graph.getRef(ref).node(this);
        });
        this.isInited = true;
    }
    showBranchingForm() {
        this.branchingFormVisible(true);
        this.newBranchNameHasFocus(true);
    }
    showRefSearchForm(obj, event) {
        this.refSearchFormVisible(true);
        const textBox = event.currentTarget.parentElement.querySelector('input[type="search"]');
        const $textBox = $(textBox);
        if (!$textBox.autocomplete('instance')) {
            const renderItem = (ul, item) => $(`<li><a>${item.displayHtml()}</a></li>`).appendTo(ul);
            $textBox.autocomplete({
                classes: {
                    'ui-autocomplete': 'dropdown-menu',
                },
                source: this.refs().filter((ref) => !ref.isHEAD),
                minLength: 0,
                create: (event) => {
                    $(event.target).data('ui-autocomplete')._renderItem = renderItem;
                },
                select: (_event, ui) => {
                    const ref = ui.item;
                    const ray = ref.isTag ? this.tagsToDisplay : this.branchesToDisplay;
                    // if ref is in display, remove it, else remove last in array.
                    ray.splice(ray.indexOf(ref), 1);
                    ray.unshift(ref);
                    this.refSearchFormVisible(false);
                    // Clear search input on selection
                    return false;
                },
            });
            $textBox.on('focus', (event) => {
                $(event.target).autocomplete('search', event.target.value);
            });
            $textBox.autocomplete('search', '');
        }
    }
    createBranch() {
        if (!this.canCreateRef())
            return;
        this.graph.server
            .postPromise('/branches', {
            path: this.graph.repoPath(),
            name: this.newBranchName(),
            sha1: this.sha1,
        })
            .then(() => {
            this.graph.getRef(`refs/heads/${this.newBranchName()}`).node(this);
            if (ungit.config.autoCheckoutOnBranchCreate) {
                return this.graph.server.postPromise('/checkout', {
                    path: this.graph.repoPath(),
                    name: this.newBranchName(),
                });
            }
        })
            .catch((e) => this.graph.server.unhandledRejection(e))
            .finally(() => {
            this.branchingFormVisible(false);
            this.newBranchName('');
            programEvents.dispatch({ event: 'branch-updated' });
        });
    }
    createTag() {
        if (!this.canCreateRef())
            return;
        this.graph.server
            .postPromise('/tags', {
            path: this.graph.repoPath(),
            name: this.newBranchName(),
            sha1: this.sha1,
        })
            .then(() => this.graph.getRef(`refs/tags/${this.newBranchName()}`).node(this))
            .catch((e) => this.graph.server.unhandledRejection(e))
            .finally(() => {
            this.branchingFormVisible(false);
            this.newBranchName('');
        });
    }
    toggleSelected() {
        this.selected(!this.selected());
        if (this.selected()) {
            const commitElement = this.commitComponent.element();
            const commitRect = commitElement.getBoundingClientRect();
            if (commitRect.top <
                +window.getComputedStyle(document.documentElement).scrollPaddingTop.replace('px', '') ||
                commitRect.bottom > document.documentElement.clientHeight) {
                commitElement.scrollIntoView();
            }
        }
        return false;
    }
    removeRef(ref) {
        if (ref.isRemoteTag) {
            this.remoteTags.remove(ref);
        }
        else {
            this.branchesAndLocalTags.remove(ref);
        }
    }
    pushRef(ref) {
        if (ref.isRemoteTag && !this.remoteTags().includes(ref)) {
            this.remoteTags.push(ref);
        }
        else if (!this.branchesAndLocalTags().includes(ref)) {
            this.branchesAndLocalTags.push(ref);
        }
    }
    updateAnimationFrame(deltaT) {
        this.commitComponent.updateAnimationFrame(deltaT);
    }
    getPathToCommonAncestor(node) {
        const path = [];
        let thisNode = this;
        while (thisNode && !node.isAncestor(thisNode)) {
            path.push(thisNode);
            thisNode = this.graph.nodesById[thisNode.parents()[0]];
        }
        if (thisNode)
            path.push(thisNode);
        return path;
    }
    isAncestor(node) {
        if (node == this)
            return true;
        for (const v in this.parents()) {
            const n = this.graph.nodesById[this.parents()[v]];
            if (n && n.isAncestor(node))
                return true;
        }
        return false;
    }
    getRightToLeftStrike() {
        return `M ${this.cx() - 30} ${this.cy() - 30} L ${this.cx() + 30} ${this.cy() + 30}`;
    }
    getLeftToRightStrike() {
        return `M ${this.cx() + 30} ${this.cy() - 30} L ${this.cx() - 30} ${this.cy() + 30}`;
    }
    nodePointerEnter(data, event) {
        // Touch-triggered hover inserts commit details and prevents iOS Safari's synthetic click.
        if (event.pointerType === 'mouse')
            this.nodeIsMousehover(true);
    }
    nodePointerLeave(data, event) {
        if (event.pointerType === 'mouse')
            this.nodeIsMousehover(false);
    }
    isViewable() {
        return this.graph.nodes().includes(this);
    }
}
module.exports = GitNodeViewModel;

},{"./animateable":2,"./git-graph-actions":4,"jquery":undefined,"knockout":undefined,"ungit-components":undefined,"ungit-program-events":undefined,"ungit-theme":undefined}],6:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const octicons = require('octicons');
const theme = require('ungit-theme');
const programEvents = require('ungit-program-events');
const components = require('ungit-components');
const Selectable = require('./selectable');
class RefViewModel extends Selectable {
    constructor(fullRefName, graph) {
        super(graph);
        this.graph = graph;
        this.name = fullRefName;
        this.node = ko.observable();
        this.localRefName = this.name; // origin/master or master
        this.refName = this.name; // master
        this.isRemoteTag = this.name.indexOf('remote-tag: ') == 0;
        this.isLocalTag = this.name.indexOf('tag: ') == 0;
        this.isTag = this.isLocalTag || this.isRemoteTag;
        const isRemoteBranchOrHEAD = this.name.indexOf('refs/remotes/') == 0;
        this.isLocalHEAD = this.name == 'HEAD';
        this.isRemoteHEAD = this.name.includes('/HEAD');
        this.isLocalBranch = this.name.indexOf('refs/heads/') == 0;
        this.isRemoteBranch = isRemoteBranchOrHEAD && !this.isRemoteHEAD;
        this.isStash = this.name.indexOf('refs/stash') == 0;
        this.isHEAD = this.isLocalHEAD || this.isRemoteHEAD;
        this.isBranch = this.isLocalBranch || this.isRemoteBranch;
        this.isRemote = isRemoteBranchOrHEAD || this.isRemoteTag;
        this.isLocal = this.isLocalBranch || this.isLocalTag;
        if (this.isLocalBranch) {
            this.localRefName = this.name.slice('refs/heads/'.length);
            this.refName = this.localRefName;
        }
        if (this.isRemoteBranch) {
            this.localRefName = this.name.slice('refs/remotes/'.length);
        }
        if (this.isLocalTag) {
            this.localRefName = this.name.slice('tag: refs/tags/'.length);
            this.refName = this.localRefName;
        }
        if (this.isRemoteTag) {
            this.localRefName = this.name.slice('remote-tag: '.length);
        }
        const splitedName = this.localRefName.split('/');
        if (this.isRemote) {
            // get rid of the origin/ part of origin/branchname
            this.remote = splitedName[0];
            this.refName = splitedName.slice(1).join('/');
        }
        this.show = true;
        this.server = this.graph.server;
        this.isDragging = ko.observable(false);
        this.current = ko.computed(() => this.isLocalBranch && this.graph.checkedOutBranch() == this.refName);
        this.color = ko.computed(() => this._colorFromHashOfString(this.name));
        this.node.subscribe((oldNode) => {
            if (oldNode)
                oldNode.removeRef(this);
        }, null, 'beforeChange');
        this.node.subscribe((newNode) => {
            if (newNode)
                newNode.pushRef(this);
        });
        // This optimization is for autocomplete display
        this.value = splitedName[splitedName.length - 1];
        this.label = this.localRefName;
        this.displayHtml = (largeCurrent) => {
            const size = largeCurrent && this.current() ? 26 : 18;
            let prefix = '';
            if (this.isRemote) {
                prefix = `<span>${octicons.globe.toSVG({ height: size })}</span> `;
            }
            if (this.isBranch) {
                prefix += `<span>${octicons['git-branch'].toSVG({ height: size })}</span> `;
            }
            else if (this.isTag) {
                prefix += `<span>${octicons.tag.toSVG({ height: size })}</span> `;
            }
            return prefix + this.localRefName;
        };
    }
    _colorFromHashOfString(string) {
        return theme.colorForRef(string);
    }
    dragStart() {
        this.graph.currentActionContext(this);
        this.isDragging(true);
        if (document.activeElement)
            document.activeElement.blur();
    }
    dragEnd() {
        this.graph.currentActionContext(null);
        this.isDragging(false);
    }
    moveTo(target, rewindWarnOverride) {
        let promise;
        if (this.isLocal) {
            const toNode = this.graph.nodesById[target];
            const args = {
                path: this.graph.repoPath(),
                name: this.refName,
                sha1: target,
                force: true,
                to: target,
                mode: 'hard',
            };
            let operation;
            if (this.current()) {
                operation = '/reset';
            }
            else if (this.isTag) {
                operation = '/tags';
            }
            else {
                operation = '/branches';
            }
            if (!rewindWarnOverride && this.node().date > toNode.date) {
                promise = new Promise((resolve, reject) => {
                    components.showModal('yesnomodal', {
                        title: 'Are you sure?',
                        details: 'This operation potentially going back in history.',
                        closeFunc: (isYes) => {
                            if (isYes) {
                                return this.server.postPromise(operation, args).then(resolve).catch(reject);
                            }
                        },
                    });
                });
            }
            else {
                promise = this.server.postPromise(operation, args);
            }
        }
        else {
            const pushReq = {
                path: this.graph.repoPath(),
                remote: this.remote,
                refSpec: target,
                remoteBranch: this.refName,
            };
            promise = this.server.postPromise('/push', pushReq).catch((err) => {
                if (err.errorCode === 'non-fast-forward') {
                    return new Promise((resolve, reject) => {
                        components.showModal('yesnomodal', {
                            title: 'Force push?',
                            details: "The remote branch can't be fast-forwarded.",
                            closeFunc: (isYes) => {
                                if (!isYes)
                                    return resolve(false);
                                pushReq.force = true;
                                this.server.postPromise('/push', pushReq).then(resolve).catch(reject);
                            },
                        });
                    });
                }
                else {
                    this.server.unhandledRejection(err);
                }
            });
        }
        return promise
            .then((res) => {
            if (!res)
                return;
            const targetNode = this.graph.getNode(target);
            if (this.graph.checkedOutBranch() == this.refName) {
                this.graph.HEADref().node(targetNode);
            }
            this.node(targetNode);
        })
            .catch((e) => this.server.unhandledRejection(e));
    }
    remove(isClientOnly) {
        let url = this.isTag ? '/tags' : '/branches';
        if (this.isRemote)
            url = `/remote${url}`;
        return (isClientOnly
            ? Promise.resolve()
            : this.server.delPromise(url, {
                path: this.graph.repoPath(),
                remote: this.isRemote ? this.remote : null,
                name: this.refName,
            }))
            .then(() => {
            if (this.node())
                this.node().removeRef(this);
            this.graph.refs.remove(this);
            delete this.graph.refsByRefName[this.name];
        })
            .catch((e) => this.server.unhandledRejection(e))
            .finally(() => {
            if (!isClientOnly) {
                if (url == '/remote/tags') {
                    programEvents.dispatch({ event: 'request-fetch-tags' });
                }
                else {
                    programEvents.dispatch({ event: 'branch-updated' });
                }
            }
        });
    }
    getLocalRef() {
        return this.graph.getRef(this.getLocalRefFullName(), false);
    }
    getLocalRefFullName() {
        if (this.isRemoteBranch)
            return `refs/heads/${this.refName}`;
        if (this.isRemoteTag)
            return `tag: ${this.refName}`;
        return null;
    }
    getRemoteRef(remote) {
        return this.graph.getRef(this.getRemoteRefFullName(remote), false);
    }
    getRemoteRefFullName(remote) {
        if (this.isLocalBranch)
            return `refs/remotes/${remote}/${this.refName}`;
        if (this.isLocalTag)
            return `remote-tag: ${remote}/${this.refName}`;
        return null;
    }
    canBePushed(remote) {
        if (!this.isLocal)
            return false;
        if (!remote)
            return false;
        const remoteRef = this.getRemoteRef(remote);
        if (!remoteRef)
            return true;
        return this.node() != remoteRef.node();
    }
    createRemoteRef() {
        return this.server
            .postPromise('/push', {
            path: this.graph.repoPath(),
            remote: this.graph.currentRemote(),
            refSpec: this.refName,
            remoteBranch: this.refName,
        })
            .catch((e) => this.server.unhandledRejection(e));
    }
    checkout() {
        const isRemote = this.isRemoteBranch;
        const isLocalCurrent = this.getLocalRef() && this.getLocalRef().current();
        return Promise.resolve()
            .then(() => {
            if (isRemote && !isLocalCurrent) {
                return this.server.postPromise('/branches', {
                    path: this.graph.repoPath(),
                    name: this.refName,
                    sha1: this.name,
                    force: true,
                });
            }
        })
            .then(() => this.server.postPromise('/checkout', { path: this.graph.repoPath(), name: this.refName }))
            .then(() => {
            if (isRemote && isLocalCurrent) {
                return this.server.postPromise('/reset', {
                    path: this.graph.repoPath(),
                    to: this.name,
                    mode: 'hard',
                });
            }
        })
            .then(() => {
            this.graph.HEADref().node(this.node());
        })
            .catch((err) => {
            if (err.errorCode != 'merge-failed') {
                this.server.unhandledRejection(err);
            }
            else {
                ungit.logger.warn('checkout failed', err);
            }
        });
    }
}
module.exports = RefViewModel;

},{"./selectable":9,"knockout":undefined,"octicons":undefined,"ungit-components":undefined,"ungit-program-events":undefined,"ungit-theme":undefined}],7:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const _ = require('lodash');
const moment = require('moment');
const octicons = require('octicons');
const components = require('ungit-components');
const theme = require('ungit-theme');
const GitNodeViewModel = require('./git-node');
const GitRefViewModel = require('./git-ref');
const EdgeViewModel = require('./edge');
const { ComponentRoot } = require('../ComponentRoot');
const numberOfNodesPerLoad = ungit.config.numberOfNodesPerLoad;
components.register('graph', (args) => new GraphViewModel(args.server, args.repoPath));
class GraphViewModel extends ComponentRoot {
    constructor(server, repoPath) {
        super();
        this._isLoadNodesFromApiRunning = false;
        this._loadNodesFromApiPending = false;
        this._loadNodesGeneration = 0;
        this.updateBranches = _.debounce(this._updateBranches, 250, this.defaultDebounceOption);
        this._debouncedLoadNodesFromApi = _.debounce(() => this._drainLoadNodesFromApi(), 250, this.defaultDebounceOption);
        this.loadNodesFromApi = () => {
            this._loadNodesGeneration++;
            this._loadNodesFromApiPending = true;
            this._debouncedLoadNodesFromApi();
        };
        this._markIdeologicalStamp = 0;
        this.repoPath = repoPath;
        this.limit = ko.observable(numberOfNodesPerLoad);
        this.skip = ko.observable(0);
        this.server = server;
        this.currentRemote = ko.observable();
        this.nodes = ko.observableArray();
        this.edges = ko.observableArray();
        this.refs = ko.observableArray();
        this.nodesById = {};
        this.edgesById = {};
        this.refsByRefName = {};
        this.checkedOutBranch = ko.observable();
        this.checkedOutRef = ko.computed(() => this.checkedOutBranch() ? this.getRef(`refs/heads/${this.checkedOutBranch()}`) : null);
        this.HEADref = ko.observable();
        this.HEAD = ko.computed(() => (this.HEADref() ? this.HEADref().node() : undefined));
        this.graphEdgeColor = ko.computed(() => theme.graphEdgeColor());
        this.graphAccentColor = ko.computed(() => theme.graphAccentColor());
        this.commitNodeColor = ko.computed(() => this.HEAD() ? this.HEAD().color() : theme.graphFallbackColor());
        this.commitNodeEdge = ko.computed(() => {
            if (!this.HEAD() || !this.HEAD().cx() || !this.HEAD().cy())
                return;
            return `M 610 68 L ${this.HEAD().cx()} ${this.HEAD().cy()}`;
        });
        this.currentActionContext = ko.observable();
        this.scrolledToEnd = _.debounce(() => {
            this.limit(numberOfNodesPerLoad + this.limit());
            this.loadNodesFromApi();
        }, 500, true);
        this.loadAhead = _.debounce(() => {
            if (this.skip() <= 0)
                return;
            this.skip(Math.max(this.skip() - numberOfNodesPerLoad, 0));
            this.loadNodesFromApi();
        }, 500, true);
        this.commitOpacity = ko.observable(1.0);
        this.heighstBranchOrder = 0;
        this.hoverGraphActionGraphic = ko.observable();
        this.hoverGraphActionGraphic.subscribe((value) => {
            if (value && value.destroy)
                value.destroy();
        }, null, 'beforeChange');
        this.hoverGraphAction = ko.observable();
        this.hoverGraphAction.subscribe((value) => {
            if (value && value.createHoverGraphic) {
                this.hoverGraphActionGraphic(value.createHoverGraphic());
            }
            else {
                this.hoverGraphActionGraphic(null);
            }
        });
        this.loadNodesFromApi();
        this.updateBranches();
        this.graphWidth = ko.observable();
        this.graphHeight = ko.observable(800);
        this.searchIcon = octicons.search.toSVG({ height: 18 });
        this.plusIcon = octicons.plus.toSVG({ height: 18 });
    }
    updateNode(parentElement) {
        ko.renderTemplate('graph', this, {}, parentElement);
    }
    getNode(sha1, logEntry) {
        let nodeViewModel = this.nodesById[sha1];
        if (!nodeViewModel)
            nodeViewModel = this.nodesById[sha1] = new GitNodeViewModel(this, sha1);
        if (logEntry)
            nodeViewModel.setData(logEntry);
        return nodeViewModel;
    }
    getRef(ref, constructIfUnavailable) {
        if (constructIfUnavailable === undefined)
            constructIfUnavailable = true;
        let refViewModel = this.refsByRefName[ref];
        if (!refViewModel && constructIfUnavailable) {
            refViewModel = this.refsByRefName[ref] = new GitRefViewModel(ref, this);
            this.refs.push(refViewModel);
            if (refViewModel.name === 'HEAD') {
                this.HEADref(refViewModel);
            }
        }
        return refViewModel;
    }
    async _drainLoadNodesFromApi() {
        if (this._isLoadNodesFromApiRunning)
            return;
        this._isLoadNodesFromApiRunning = true;
        try {
            while (this._loadNodesFromApiPending) {
                this._loadNodesFromApiPending = false;
                const generation = this._loadNodesGeneration;
                await this._loadNodesFromApi(generation);
            }
        }
        finally {
            this._isLoadNodesFromApiRunning = false;
        }
    }
    async _loadNodesFromApi(generation) {
        ungit.logger.debug(`graph.loadNodesFromApi() triggered (generation ${generation})`);
        const nodeSize = this.nodes().length;
        const edges = [];
        const limit = this.limit();
        const skip = this.skip();
        try {
            const log = await this.server.getPromise('/gitlog', {
                path: this.repoPath(),
                limit: limit,
                skip: skip,
            });
            // Realtime/fetch/scroll events can request a newer graph while this Git log
            // is still running. Never let the older response overwrite the newer view.
            if (generation !== this._loadNodesGeneration) {
                ungit.logger.debug(`graph.loadNodesFromApi() discarded stale generation ${generation}; latest is ${this._loadNodesGeneration}`);
                return;
            }
            if (this.isSamePayload(log)) {
                return;
            }
            const nodes = this.computeNode((log.nodes || []).map((logEntry) => {
                return this.getNode(logEntry.sha1, logEntry); // convert to node object
            }));
            // create edges
            nodes.forEach((node) => {
                node.parents().forEach((parentSha1) => {
                    edges.push(this.getEdge(node.sha1, parentSha1));
                });
                node.render();
            });
            this.edges(edges);
            this.nodes(nodes);
            if (nodes.length > 0) {
                this.graphHeight(nodes[nodes.length - 1].cy() + 80);
            }
            this.graphWidth(1000 + this.heighstBranchOrder * 90);
        }
        catch (e) {
            // Errors from superseded loads are stale too; only surface the current one.
            if (generation === this._loadNodesGeneration) {
                this.server.unhandledRejection(e);
            }
        }
        finally {
            if (generation === this._loadNodesGeneration &&
                window.innerHeight - this.graphHeight() > 0 &&
                nodeSize != this.nodes().length) {
                this.scrolledToEnd();
            }
            ungit.logger.debug(`graph.loadNodesFromApi() finished (generation ${generation})`);
        }
    }
    traverseNodeLeftParents(node, callback) {
        callback(node);
        const parent = this.nodesById[node.parents()[0]];
        if (parent) {
            this.traverseNodeLeftParents(parent, callback);
        }
    }
    computeNode(nodes) {
        this.markNodesIdeologicalBranches(this.refs());
        const updateTimeStamp = moment().valueOf();
        if (this.HEAD()) {
            this.traverseNodeLeftParents(this.HEAD(), (node) => {
                node.ancestorOfHEADTimeStamp = updateTimeStamp;
            });
        }
        // Filter out nodes which doesn't have a branch (staging and orphaned nodes)
        nodes = nodes.filter((node) => (node.ideologicalBranch() && !node.ideologicalBranch().isStash) ||
            node.ancestorOfHEADTimeStamp == updateTimeStamp);
        let branchSlotCounter = this.HEAD() ? 1 : 0;
        // Then iterate from the bottom to fix the orders of the branches
        for (let i = nodes.length - 1; i >= 0; i--) {
            const node = nodes[i];
            if (node.ancestorOfHEADTimeStamp == updateTimeStamp)
                continue;
            const ideologicalBranch = node.ideologicalBranch();
            // First occurrence of the branch, find an empty slot for the branch
            if (ideologicalBranch.lastSlottedTimeStamp != updateTimeStamp) {
                ideologicalBranch.lastSlottedTimeStamp = updateTimeStamp;
                ideologicalBranch.branchOrder = branchSlotCounter++;
            }
            node.branchOrder(ideologicalBranch.branchOrder);
        }
        this.heighstBranchOrder = branchSlotCounter - 1;
        let prevNode;
        nodes.forEach((node) => {
            node.ancestorOfHEAD(node.ancestorOfHEADTimeStamp == updateTimeStamp);
            if (node.ancestorOfHEAD())
                node.branchOrder(0);
            node.aboveNode = prevNode;
            if (prevNode)
                prevNode.belowNode = node;
            prevNode = node;
        });
        return nodes;
    }
    getEdge(nodeAsha1, nodeBsha1) {
        const id = `${nodeAsha1}-${nodeBsha1}`;
        let edge = this.edgesById[id];
        if (!edge) {
            edge = this.edgesById[id] = new EdgeViewModel(this, nodeAsha1, nodeBsha1);
        }
        return edge;
    }
    markNodesIdeologicalBranches(refs) {
        refs = refs.filter((r) => !!r.node());
        refs = refs.sort((a, b) => {
            if (a.isLocal && !b.isLocal)
                return -1;
            if (b.isLocal && !a.isLocal)
                return 1;
            if (a.isBranch && !b.isBranch)
                return -1;
            if (b.isBranch && !a.isBranch)
                return 1;
            if (a.isHEAD && !b.isHEAD)
                return 1;
            if (!a.isHEAD && b.isHEAD)
                return -1;
            if (a.isStash && !b.isStash)
                return 1;
            if (b.isStash && !a.isStash)
                return -1;
            if (a.node() && a.node().date && b.node() && b.node().date)
                return a.node().date - b.node().date;
            return a.refName < b.refName ? -1 : 1;
        });
        const stamp = this._markIdeologicalStamp++;
        refs.forEach((ref) => {
            this.traverseNodeParents(ref.node(), (node) => {
                if (node.stamp == stamp)
                    return false;
                node.stamp = stamp;
                node.ideologicalBranch(ref);
                return true;
            });
        });
    }
    traverseNodeParents(node, callback) {
        if (!callback(node))
            return false;
        for (let i = 0; i < node.parents().length; i++) {
            // if parent, travers parent
            const parent = this.nodesById[node.parents()[i]];
            if (parent) {
                this.traverseNodeParents(parent, callback);
            }
        }
    }
    handleBubbledClick(elem, event) {
        // If the clicked element is bound to the current action context,
        // then let's not deselect it.
        if (ko.dataFor(event.target) === this.currentActionContext())
            return;
        if (this.currentActionContext() && this.currentActionContext() instanceof GitNodeViewModel) {
            this.currentActionContext().toggleSelected();
        }
        else {
            this.currentActionContext(null);
        }
        // If the click was on an input element, then let's allow the default action to proceed.
        // This is especially needed since for some strange reason any submit (ie. enter in a textbox)
        // will trigger a click event on the submit input of the form, which will end up here,
        // and if we don't return true, then the submit event is never fired, breaking stuff.
        if (event.target.nodeName === 'INPUT')
            return true;
    }
    onProgramEvent(event) {
        if (event.event == 'git-directory-changed' || event.event === 'working-tree-changed') {
            this.loadNodesFromApi();
            this.updateBranches();
        }
        else if (event.event == 'request-app-content-refresh') {
            this.loadNodesFromApi();
        }
        else if (event.event == 'remote-tags-update') {
            this.setRemoteTags(event.tags);
        }
        else if (event.event == 'current-remote-changed') {
            this.currentRemote(event.newRemote);
        }
        else if (event.event == 'graph-render' || event.event === 'theme-changed') {
            this.nodes().forEach((node) => {
                node.render();
            });
        }
    }
    updateAnimationFrame(deltaT) {
        this.nodes().forEach((node) => {
            node.updateAnimationFrame(deltaT);
        });
    }
    async _updateBranches() {
        const checkout = await this.server.getPromise('/checkout', { path: this.repoPath() });
        try {
            ungit.logger.debug('setting checkedOutBranch', checkout);
            this.checkedOutBranch(checkout);
        }
        catch (err) {
            if (err.errorCode != 'not-a-repository') {
                this.server.unhandledRejection(err);
            }
            else {
                ungit.logger.warn('updateBranches failed', err);
            }
        }
    }
    setRemoteTags(remoteTags) {
        const version = Date.now();
        const sha1Map = {}; // map holding true sha1 per tags
        remoteTags.forEach((tag) => {
            if (tag.name.includes('^{}')) {
                // This tag is a dereference tag, use this sha1.
                const tagRef = tag.name.slice(0, tag.name.length - '^{}'.length);
                sha1Map[tagRef] = tag.sha1;
            }
            else if (!sha1Map[tag.name]) {
                // If sha1 wasn't previously set, use this sha1
                sha1Map[tag.name] = tag.sha1;
            }
        });
        remoteTags.forEach((ref) => {
            if (!ref.name.includes('^{}')) {
                const name = `remote-tag: ${ref.remote}/${ref.name.split('/')[2]}`;
                this.getRef(name).node(this.getNode(sha1Map[ref.name]));
                this.getRef(name).version = version;
            }
        });
        this.refs().forEach((ref) => {
            // tag is removed from another source
            if (ref.isRemoteTag && (!ref.version || ref.version < version)) {
                ref.remove(true);
            }
        });
    }
    checkHeadMove(toNode) {
        if (this.HEAD() === toNode) {
            this.HEADref().node(toNode);
        }
    }
}
module.exports = GraphViewModel;

},{"../ComponentRoot":1,"./edge":3,"./git-node":5,"./git-ref":6,"knockout":undefined,"lodash":undefined,"moment":undefined,"octicons":undefined,"ungit-components":undefined,"ungit-theme":undefined}],8:[function(require,module,exports){
"use strict";
const theme = require('ungit-theme');
const getEdgeModelWithD = (d, stroke, strokeWidth, strokeDasharray, markerEnd) => ({
    d,
    stroke: stroke ? stroke : theme.graphEdgeColor(),
    strokeWidth: strokeWidth ? strokeWidth : '8',
    strokeDasharray: strokeDasharray ? strokeDasharray : '10, 5',
    markerEnd: markerEnd ? markerEnd : '',
});
const getEdgeModel = (scx, scy, tcx, tcy, stroke, strokeWidth, strokeDasharray, markerEnd) => {
    return getEdgeModelWithD(`M ${scx} ${scy} L ${tcx} ${tcy}`, stroke, strokeWidth, strokeDasharray, markerEnd);
};
const getNodeModel = (cx, cy, r, fill, stroke, strokeWidth, strokeDasharray) => ({
    cx,
    cy,
    r,
    fill,
    stroke: stroke ? stroke : '#41DE3C',
    strokeWidth: strokeWidth ? strokeWidth : '8',
    strokeDasharray: strokeDasharray ? strokeDasharray : '10, 5',
});
class HoverViewModel {
    constructor() {
        this.bgEdges = [];
        this.nodes = [];
        this.fgEdges = [];
    }
}
class MergeViewModel extends HoverViewModel {
    constructor(graph, headNode, node) {
        super();
        this.graph = graph;
        this.bgEdges = [
            getEdgeModel(headNode.cx(), headNode.cy() - 110, headNode.cx(), headNode.cy()),
            getEdgeModel(headNode.cx(), headNode.cy() - 110, node.cx(), node.cy()),
        ];
        this.nodes = [
            getNodeModel(headNode.cx(), headNode.cy() - 110, Math.max(headNode.r(), node.r()), theme.graphAccentColor(), '#41DE3C', '8', '10, 5'),
        ];
        graph.commitOpacity(0.1);
    }
    destroy() {
        this.graph.commitOpacity(1.0);
    }
}
exports.MergeViewModel = MergeViewModel;
class RebaseViewModel extends HoverViewModel {
    constructor(onto, nodesThatWillMove) {
        super();
        nodesThatWillMove = nodesThatWillMove.slice(0, -1);
        if (nodesThatWillMove.length == 0)
            return;
        this.bgEdges.push(getEdgeModel(onto.cx(), onto.cy(), onto.cx(), onto.cy() - 60));
        nodesThatWillMove.forEach((node, i) => {
            const cy = onto.cy() + -90 * (i + 1);
            this.nodes.push(getNodeModel(onto.cx(), cy, 28, 'transparent'));
            if (i + 1 < nodesThatWillMove.length) {
                this.bgEdges.push(getEdgeModel(onto.cx(), cy - 25, onto.cx(), cy - 65));
            }
        });
    }
}
exports.RebaseViewModel = RebaseViewModel;
class ResetViewModel extends HoverViewModel {
    constructor(nodes) {
        super();
        nodes.forEach((node) => {
            this.fgEdges.push(getEdgeModelWithD(node.getLeftToRightStrike(), 'rgb(255, 129, 31)', '8', '0, 0'));
            this.fgEdges.push(getEdgeModelWithD(node.getRightToLeftStrike(), 'rgb(255, 129, 31)', '8', '0, 0'));
        });
    }
}
exports.ResetViewModel = ResetViewModel;
class PushViewModel extends HoverViewModel {
    constructor(fromNode, toNode) {
        super();
        this.fgEdges = [
            getEdgeModel(fromNode.cx(), fromNode.cy(), toNode.cx(), toNode.cy() + 40, 'rgb(61, 139, 255)', '15', '10, 5', 'url(#pushArrowEnd)'),
        ];
    }
}
exports.PushViewModel = PushViewModel;
class SquashViewModel extends HoverViewModel {
    constructor(from, onto) {
        super();
        let path = from.getPathToCommonAncestor(onto);
        if (path.length == 0) {
            return;
        }
        else if (path.length == 1) {
            path = onto.getPathToCommonAncestor(from);
        }
        else {
            this.nodes.push(getNodeModel(onto.cx(), onto.cy() - 120, 28, 'transparent'));
        }
        path.slice(0, -1).forEach((node) => {
            this.nodes.push(getNodeModel(node.cx(), node.cy(), node.r() + 2, 'rgba(100, 60, 222, 0.8)'));
        });
    }
}
exports.SquashViewModel = SquashViewModel;

},{"ungit-theme":undefined}],9:[function(require,module,exports){
"use strict";
var ko = require('knockout');
class Selectable {
    constructor(graph) {
        this.selected = ko.computed({
            read() {
                return graph.currentActionContext() == this;
            },
            write(val) {
                // val is this if we're called from a click ko binding
                if (val === this || val === true) {
                    graph.currentActionContext(this);
                }
                else if (graph.currentActionContext() == this) {
                    graph.currentActionContext(null);
                }
            },
            owner: this,
        });
    }
}
module.exports = Selectable;

},{"knockout":undefined}]},{},[7])
//# sourceMappingURL=graph.bundle.js.map
