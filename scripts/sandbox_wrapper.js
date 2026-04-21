#!/usr/bin/env node
/**
 * Genoma Sandbox Wrapper — Node.js
 * Executes a user script in the sandbox protocol format.
 * Communication: stdin (JSON) → script execution → stdout (JSON-lines)
 */

const fs = require('fs');
const path = require('path');
const { Module } = require('module');

function emit(type, data, traceback = '') {
    const msg = { type, data };
    if (traceback) msg.traceback = traceback;
    process.stdout.write(JSON.stringify(msg) + '\n');
}

async function main() {
    try {
        // Read input from stdin
        const chunks = [];
        for await (const chunk of process.stdin) {
            chunks.push(chunk);
        }
        const rawInput = Buffer.concat(chunks).toString('utf-8').trim();

        if (!rawInput) {
            emit('error', 'No input received on stdin');
            process.exit(1);
        }

        const payload = JSON.parse(rawInput);
        const scriptPath = payload.script_path || '/workspace/script.js';
        const userInput = payload.input || {};

        // Verify script exists
        if (!fs.existsSync(scriptPath)) {
            emit('error', `Script not found: ${scriptPath}`);
            process.exit(1);
        }

        // Capture console.log output
        const logs = [];
        const originalLog = console.log;
        const originalError = console.error;
        const originalWarn = console.warn;

        console.log = (...args) => {
            const msg = args.map(a => typeof a === 'string' ? a : JSON.stringify(a)).join(' ');
            emit('log', msg);
        };
        console.error = (...args) => {
            const msg = args.map(a => typeof a === 'string' ? a : JSON.stringify(a)).join(' ');
            emit('log', `[stderr] ${msg}`);
        };
        console.warn = console.error;

        // Make INPUT available globally
        global.INPUT = userInput;
        global.emit_log = (msg) => emit('log', String(msg));

        // Load and execute the script
        const scriptContent = fs.readFileSync(scriptPath, 'utf-8');

        // Create a module sandbox
        const scriptModule = new Module(scriptPath);
        scriptModule.filename = scriptPath;
        scriptModule.paths = Module._nodeModulePaths(path.dirname(scriptPath));

        // Execute
        const wrapper = Module.wrap(scriptContent);
        const compiled = require('vm').runInThisContext(wrapper, { filename: scriptPath });
        compiled.call(scriptModule.exports, scriptModule.exports, require, scriptModule, scriptPath, path.dirname(scriptPath));

        // Get result
        let result;
        if (typeof scriptModule.exports === 'function') {
            result = await scriptModule.exports(userInput);
        } else if (scriptModule.exports && typeof scriptModule.exports.main === 'function') {
            result = await scriptModule.exports.main(userInput);
        } else if (scriptModule.exports && typeof scriptModule.exports !== 'object') {
            result = { value: scriptModule.exports };
        } else if (scriptModule.exports.RESULT !== undefined) {
            result = scriptModule.exports.RESULT;
        } else {
            result = { status: 'completed' };
        }

        if (typeof result !== 'object' || result === null) {
            result = { value: result };
        }

        emit('result', result);

        // Restore console
        console.log = originalLog;
        console.error = originalError;
        console.warn = originalWarn;

    } catch (err) {
        emit('error', err.message, err.stack);
        process.exit(1);
    }
}

main();
