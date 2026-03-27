const puppeteer = require('puppeteer');
const fs = require('fs');
const path = require('path');

const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

async function testApplication() {
  console.log('Starting browser...');
  const browser = await puppeteer.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage']
  });
  
  const page = await browser.newPage();
  await page.setViewport({ width: 1920, height: 1080 });
  
  const screenshotDir = path.join(__dirname, 'test-screenshots');
  if (!fs.existsSync(screenshotDir)) {
    fs.mkdirSync(screenshotDir);
  }
  
  try {
    console.log('\n=== Test 1: Overview Page ===');
    await page.goto('http://localhost:8080', { waitUntil: 'domcontentloaded', timeout: 10000 });
    
    // Wait for the page to finish loading by checking if DATA is populated
    console.log('Waiting for data to load...');
    try {
      await page.waitForFunction(() => {
        return typeof DATA !== 'undefined' && DATA !== null;
      }, { timeout: 20000 });
      console.log('✓ Data loaded successfully');
    } catch (e) {
      console.log('⚠ Timeout waiting for data, taking screenshot anyway...');
    }
    
    await delay(2000); // Extra wait for rendering
    await page.screenshot({ path: path.join(screenshotDir, '1-overview.png'), fullPage: true });
    console.log('✓ Screenshot saved: 1-overview.png');
    
    // Get page info
    const pageInfo = await page.evaluate(() => {
      const results = {
        healthScore: 'Not found',
        dataLoaded: typeof DATA !== 'undefined' && DATA !== null,
        tabs: []
      };
      
      // Try to find health score
      if (DATA && DATA.scores) {
        results.healthScore = DATA.scores.overall;
      }
      
      // Get tabs
      const buttons = document.querySelectorAll('button');
      buttons.forEach(btn => {
        const text = btn.textContent.trim();
        if (text && text.length > 2 && text.length < 50 && !['🌓', 'Export', '🔄', '✕'].includes(text)) {
          results.tabs.push(text);
        }
      });
      
      return results;
    });
    
    console.log(`Data Loaded: ${pageInfo.dataLoaded}`);
    console.log(`Cluster Health Score: ${pageInfo.healthScore}`);
    console.log(`Available tabs: ${pageInfo.tabs.slice(0, 10).join(', ')}`);
    
    if (!pageInfo.dataLoaded) {
      console.log('\n⚠ WARNING: Data did not load properly. The API might not be responding.');
      console.log('This could be due to:');
      console.log('1. Port-forward connection issues');
      console.log('2. API taking too long to respond');
      console.log('3. JavaScript execution issues');
      return;
    }
    
    console.log('\n=== Test 2: Pod Health Tab ===');
    const podHealthClicked = await page.evaluate(() => {
      const buttons = document.querySelectorAll('button');
      for (const btn of buttons) {
        const text = btn.textContent.trim();
        if (text.includes('💊') || text.toLowerCase().includes('pod')) {
          btn.click();
          return text;
        }
      }
      return null;
    });
    
    if (podHealthClicked) {
      console.log(`✓ Clicked tab: ${podHealthClicked}`);
      await delay(1500);
      await page.screenshot({ path: path.join(screenshotDir, '2-pod-health.png'), fullPage: true });
      console.log('✓ Screenshot saved: 2-pod-health.png');
      
      // Check for Diagnose buttons
      const podInfo = await page.evaluate(() => {
        const buttons = document.querySelectorAll('button');
        let diagnoseCount = 0;
        buttons.forEach(btn => {
          if (btn.textContent.includes('Diagnose')) diagnoseCount++;
        });
        return { diagnoseCount };
      });
      console.log(`Found ${podInfo.diagnoseCount} Diagnose buttons`);
      
      if (podInfo.diagnoseCount > 0) {
        await page.evaluate(() => {
          const buttons = document.querySelectorAll('button');
          for (const btn of buttons) {
            if (btn.textContent.includes('Diagnose')) {
              btn.click();
              break;
            }
          }
        });
        
        console.log('✓ Clicked Diagnose button');
        await delay(2000);
        await page.screenshot({ path: path.join(screenshotDir, '3-diagnosis-modal.png'), fullPage: true });
        console.log('✓ Screenshot saved: 3-diagnosis-modal.png');
        
        // Close modal
        await page.evaluate(() => {
          const buttons = document.querySelectorAll('button');
          for (const btn of buttons) {
            if (btn.textContent.trim() === '✕') {
              btn.click();
              break;
            }
          }
        });
        await delay(500);
      } else {
        console.log('⚠ No unhealthy pods to diagnose');
      }
    } else {
      console.log('⚠ Pod Health tab not found');
    }
    
    console.log('\n=== Test 3: Resources Tab ===');
    const resourcesClicked = await page.evaluate(() => {
      const buttons = document.querySelectorAll('button');
      for (const btn of buttons) {
        const text = btn.textContent.trim();
        if (text.includes('📊') || text.toLowerCase().includes('resource')) {
          btn.click();
          return text;
        }
      }
      return null;
    });
    
    if (resourcesClicked) {
      console.log(`✓ Clicked tab: ${resourcesClicked}`);
      await delay(1500);
      await page.screenshot({ path: path.join(screenshotDir, '4-resources.png'), fullPage: true });
      console.log('✓ Screenshot saved: 4-resources.png');
      
      // Check for Apply buttons
      const resourceInfo = await page.evaluate(() => {
        const buttons = document.querySelectorAll('button');
        let applyCount = 0;
        buttons.forEach(btn => {
          if (btn.textContent.includes('Apply')) applyCount++;
        });
        return { applyCount };
      });
      console.log(`Found ${resourceInfo.applyCount} Apply buttons`);
      
      if (resourceInfo.applyCount > 0) {
        await page.evaluate(() => {
          const buttons = document.querySelectorAll('button');
          for (const btn of buttons) {
            if (btn.textContent.includes('Apply')) {
              btn.click();
              break;
            }
          }
        });
        
        console.log('✓ Clicked Apply button');
        await delay(2000);
        await page.screenshot({ path: path.join(screenshotDir, '5-resource-apply-modal.png'), fullPage: true });
        console.log('✓ Screenshot saved: 5-resource-apply-modal.png');
      } else {
        console.log('⚠ No Apply buttons found (no over-provisioned resources)');
      }
    } else {
      console.log('⚠ Resources tab not found');
    }
    
    console.log('\n=== Test Complete ===');
    console.log(`\nAll screenshots saved to: ${screenshotDir}/`);
    
  } catch (error) {
    console.error('\n❌ Error during testing:', error.message);
    await page.screenshot({ path: path.join(screenshotDir, 'error.png'), fullPage: true });
  } finally {
    await browser.close();
  }
}

testApplication().catch(console.error);
