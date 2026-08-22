(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const CommitLineDiff = require('./commitlinediff.js').CommitLineDiff;
const components = require('ungit-components');
components.register('commitDiff', (args) => new CommitDiff(args));
class CommitDiff {
    constructor(args) {
        this.sha1 = args.sha1;
        this.showDiffButtons = args.showDiffButtons;
        this.textDiffType = args.textDiffType = args.textDiffType || components.create('textdiff.type');
        this.wordWrap = args.wordWrap = args.wordWrap || components.create('textdiff.wordwrap');
        this.whiteSpace = args.whiteSpace = args.whiteSpace || components.create('textdiff.whitespace');
        this.commitLineDiffs = args.fileLineDiffs.map((fileLineDiff) => new CommitLineDiff(args, fileLineDiff));
    }
    updateNode(parentElement) {
        ko.renderTemplate('commitdiff', this, {}, parentElement);
    }
}

},{"./commitlinediff.js":2,"knockout":undefined,"ungit-components":undefined}],2:[function(require,module,exports){
"use strict";
const ko = require('knockout');
const components = require('ungit-components');
const programEvents = require('ungit-program-events');
class CommitLineDiff {
    constructor(args, fileLineDiff) {
        this.added = ko.observable(fileLineDiff.additions);
        this.removed = ko.observable(fileLineDiff.deletions);
        this.fileName = ko.observable(fileLineDiff.fileName);
        this.oldFileName = ko.observable(fileLineDiff.oldFileName);
        this.displayName = ko.observable(fileLineDiff.displayName);
        this.fileType = fileLineDiff.type;
        this.isShowingDiffs = ko.observable(false);
        this.repoPath = args.repoPath;
        this.server = args.server;
        this.sha1 = args.sha1;
        this.textDiffType = args.textDiffType;
        this.wordWrap = args.wordWrap;
        this.whiteSpace = args.whiteSpace;
        this.specificDiff = ko.observable(this.getSpecificDiff());
    }
    getSpecificDiff() {
        return components.create(`${this.fileType}diff`, {
            filename: this.fileName(),
            oldFilename: this.oldFileName(),
            repoPath: this.repoPath,
            server: this.server,
            sha1: this.sha1,
            textDiffType: this.textDiffType,
            isShowingDiffs: this.isShowingDiffs,
            whiteSpace: this.whiteSpace,
            wordWrap: this.wordWrap,
        });
    }
    fileNameClick() {
        this.isShowingDiffs(!this.isShowingDiffs());
        programEvents.dispatch({ event: 'graph-render' });
    }
}
exports.CommitLineDiff = CommitLineDiff;

},{"knockout":undefined,"ungit-components":undefined,"ungit-program-events":undefined}]},{},[1])
//# sourceMappingURL=commitdiff.bundle.js.map
