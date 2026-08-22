(function(){function r(e,n,t){function o(i,f){if(!n[i]){if(!e[i]){var c="function"==typeof require&&require;if(!f&&c)return c(i,!0);if(u)return u(i,!0);var a=new Error("Cannot find module '"+i+"'");throw a.code="MODULE_NOT_FOUND",a}var p=n[i]={exports:{}};e[i][0].call(p.exports,function(r){var n=e[i][1][r];return o(n||r)},p,p.exports,r,e,n,t)}return n[i].exports}for(var u="function"==typeof require&&require,i=0;i<t.length;i++)o(t[i]);return o}return r})()({1:[function(require,module,exports){
"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
const ko = __importStar(require("knockout"));
const modalBase_1 = require("./modalBase");
ungit.components.register('credentialsmodal', (args) => new CredentialsModalViewModel(args.remote));
ungit.components.register('addremotemodal', (arg) => new AddRemoteModalViewModel(arg.path));
ungit.components.register('addsubmodulemodal', (arg) => new AddSubmoduleModalViewModel(arg.path));
/**
 * Form receives collection of user inputs, i.e. username, password and etc.
 */
class FormModalViewModel extends modalBase_1.ModalViewModel {
    items;
    showCancel;
    template;
    constructor(title, taModalName, showCancel) {
        super(title, taModalName);
        this.items = [];
        this.showCancel = showCancel;
        this.template = 'formModal';
    }
    submit() {
        this.close();
    }
}
class CredentialsModalViewModel extends FormModalViewModel {
    constructor(remote) {
        super(`Remote ${remote} requires authentication`, 'credentials-dialog', false);
        this.items.push(new modalBase_1.FormItems('Username', ko.observable(), 'text', true));
        this.items.push(new modalBase_1.FormItems('Password', ko.observable(), 'password', false));
    }
    submit() {
        super.submit();
        ungit.programEvents.dispatch({
            event: 'request-credentials-response',
            username: this.items[0].value(),
            password: this.items[1].value(),
        });
    }
}
class AddRemoteModalViewModel extends FormModalViewModel {
    repoPath;
    constructor(path) {
        super('Add new remote', 'add-remote', true);
        this.repoPath = path;
        this.items.push(new modalBase_1.FormItems('Name', ko.observable(), 'text', true));
        this.items.push(new modalBase_1.FormItems('Url', ko.observable(), 'text', false));
    }
    async submit() {
        super.submit();
        try {
            await ungit.server.postPromise(`/remotes/${encodeURIComponent(this.items[0].value())}`, {
                path: this.repoPath,
                url: this.items[1].value(),
            });
            ungit.programEvents.dispatch({ event: 'update-remote' });
        }
        catch (e) {
            ungit.server.unhandledRejection(e);
        }
    }
}
class AddSubmoduleModalViewModel extends FormModalViewModel {
    repoPath;
    constructor(path) {
        super('Add new submodule', 'add-submodule', true);
        this.repoPath = path;
        this.items.push(new modalBase_1.FormItems('Path', ko.observable(), 'text', true));
        this.items.push(new modalBase_1.FormItems('Url', ko.observable(), 'text', false));
    }
    async submit() {
        super.submit();
        try {
            await ungit.server.postPromise('/submodules/add', {
                path: this.repoPath,
                submodulePath: this.items[0].value(),
                submoduleUrl: this.items[1].value(),
            });
            ungit.programEvents.dispatch({ event: 'submodule-fetch' });
        }
        catch (e) {
            ungit.server.unhandledRejection(e);
        }
    }
}

},{"./modalBase":2,"knockout":undefined}],2:[function(require,module,exports){
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.PromptOptions = exports.FormItems = exports.ModalViewModel = void 0;
class ModalViewModel {
    title;
    taModalName;
    timestamp = new Date().getTime();
    constructor(title, taModalName) {
        this.title = title;
        this.taModalName = taModalName;
    }
    close() {
        ungit.programEvents.dispatch({ event: 'modal-close-dialog', modal: this });
    }
}
exports.ModalViewModel = ModalViewModel;
class FormItems {
    name;
    value;
    type;
    autoFocus;
    constructor(name, value, type, autoFocus) {
        this.name = name;
        this.value = value;
        this.type = type;
        this.autoFocus = autoFocus;
    }
}
exports.FormItems = FormItems;
class PromptOptions {
    label;
    primary;
    taId;
    close;
    constructor(label, primary, taId, close) {
        this.label = label;
        this.primary = primary;
        this.taId = taId;
        this.close = close;
    }
}
exports.PromptOptions = PromptOptions;

},{}],3:[function(require,module,exports){
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
require("./forms");
require("./prompts");

},{"./forms":1,"./prompts":4}],4:[function(require,module,exports){
"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
const ko = __importStar(require("knockout"));
const modalBase_1 = require("./modalBase");
ungit.components.register('yesnomodal', (args) => new YesNoModalViewModel(args.title, args.details, args.closeFunc));
ungit.components.register('yesnomutemodal', (args) => new YesNoMuteModalViewModel(args.title, args.details, args.closeFunc));
ungit.components.register('toomanyfilesmodal', (args) => new TooManyFilesModalViewModel(args.title, args.details, args.closeFunc));
ungit.components.register('texteditmodal', (args) => new TextEditModal(args.title, args.content, args.closeFunc));
/**
 * Prompt's receives decisions from users, such as 'yes' or 'no', based on
 * button clicks.
 */
class PromptModalViewModel extends modalBase_1.ModalViewModel {
    promptOptions;
    details;
    template;
    closeFunc;
    constructor(title, taModalName, details, closeFunc) {
        super(title, taModalName);
        this.promptOptions = [];
        this.details = ko.observable(details);
        this.template = 'promptModal';
        this.closeFunc = closeFunc;
    }
    close(isYes = false, isMute = false) {
        this.closeFunc(isYes, isMute);
        super.close();
    }
    closeYes() {
        this.close(true);
    }
    closeYesMute() {
        this.close(true, true);
    }
    closeNo() {
        this.close();
    }
}
class YesNoModalViewModel extends PromptModalViewModel {
    constructor(title, details, closeFunc) {
        super(title, 'yes-no-modal', details, closeFunc);
        this.promptOptions.push(new modalBase_1.PromptOptions('Yes', true, 'yes', this.closeYes.bind(this)));
        this.promptOptions.push(new modalBase_1.PromptOptions('No', false, 'no', this.closeNo.bind(this)));
    }
}
class YesNoMuteModalViewModel extends PromptModalViewModel {
    constructor(title, details, closeFunc) {
        super(title, 'yes-no-mute-modal', details, closeFunc);
        this.promptOptions.push(new modalBase_1.PromptOptions('Yes', true, 'yes', this.closeYes.bind(this)));
        this.promptOptions.push(new modalBase_1.PromptOptions('Yes and mute for awhile', false, 'mute', this.closeYesMute.bind(this)));
        this.promptOptions.push(new modalBase_1.PromptOptions('No', false, 'no', this.closeNo.bind(this)));
    }
}
class TooManyFilesModalViewModel extends PromptModalViewModel {
    constructor(title, details, closeFunc) {
        super(title, 'yes-no-modal', details, closeFunc);
        this.promptOptions.push(new modalBase_1.PromptOptions(`Don't load`, true, 'noLoad', this.closeYes.bind(this)));
        this.promptOptions.push(new modalBase_1.PromptOptions(`Load anyway`, false, 'loadAnyway', this.closeNo.bind(this)));
    }
}
class TextEditModal extends PromptModalViewModel {
    constructor(title, details, closeFunc) {
        super(title, 'text-edit-modal', `<textarea class="text-area-content form-control" spellcheck="false" style="height: 250px; width: 100%; font-family: monospace; resize: vertical;">${details}</textarea>`, closeFunc);
        this.promptOptions.push(new modalBase_1.PromptOptions('Save', true, 'save', this.closeYes.bind(this)));
        this.promptOptions.push(new modalBase_1.PromptOptions('Cancel', false, 'cancel', this.closeNo.bind(this)));
    }
}

},{"./modalBase":2,"knockout":undefined}]},{},[3])
//# sourceMappingURL=modals.bundle.js.map
